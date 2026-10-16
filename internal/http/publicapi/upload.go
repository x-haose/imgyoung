package publicapi

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/upload"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/http/middleware"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
)

// idempotencyHeader 客户端幂等键请求头。
const idempotencyHeader = "Idempotency-Key"

// maxTTLSeconds ttl 秒数上限:config.MaxDurationHours 折算成秒,与管理面 ttl_hours 同口径。
const maxTTLSeconds = config.MaxDurationHours * 3600

// uploadResponse 上传成功的响应体。
type uploadResponse struct {
	ID  string `json:"id"`  // 对外短 ID
	URL string `json:"url"` // 图片绝对访问地址;配置了 base_url 用配置,否则按请求推导
}

// HandleHealth 健康检查端点。
func (h *Handlers) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := w.Write([]byte("ok")); err != nil {
		h.cfg.Logger.Debug("写健康检查响应失败", "err", err)
	}
}

// parseTTL 解析 ttl 取值;未显式指定时的"用户默认 > 全局默认"回退
// 由上传服务按用户行解析,本层只翻译请求。
// @Description: ttl 单位秒;"never" 或 0 表示显式永久;空表示未显式指定。
// @param raw ttl 原始取值
// @return 显式指定的有效时长(0 表示显式永久)
// @return 是否显式指定了 ttl
// @return ttl 取值合法为 true;非法(非整数、负数或超上限)为 false
func parseTTL(raw string) (ttl time.Duration, explicit, ok bool) {
	switch raw {
	case "never":
		return 0, true, true
	case "":
		return 0, false, true
	default:
		secs, err := strconv.Atoi(raw)
		if err != nil || secs < 0 || secs > maxTTLSeconds {
			return 0, false, false
		}
		return time.Duration(secs) * time.Second, true, true
	}
}

// HandleUpload 处理数据面图片上传(Bearer Token 认证)。
// @Description: 归属用户与来源 Token 由 TokenAuth 中间件解析,此处取出后
// 交给共享上传管道;取不到主体属路由装配错误。
func (h *Handlers) HandleUpload(w http.ResponseWriter, r *http.Request) {
	tp, ok := middleware.TokenPrincipalFrom(r.Context())
	if !ok {
		h.cfg.Logger.Error("上传请求缺少 Token 主体,检查路由装配")
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	h.serveUpload(w, r, tp.UserID, tp.TokenID)
}

// HandleSessionUpload 处理管理后台图片上传(会话认证)。
// @Description: 与数据面走完全相同的上传管道与错误映射;归属用户取会话主体,
// token_id 记空(非 Token 途径)。挂在 SessionAuth+CSRF 之后,取不到会话属路由装配错误。
func (h *Handlers) HandleSessionUpload(w http.ResponseWriter, r *http.Request) {
	si, ok := middleware.SessionFrom(r.Context())
	if !ok {
		h.cfg.Logger.Error("后台上传请求缺少会话主体,检查路由装配")
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	h.serveUpload(w, r, si.Principal.UserID, 0)
}

// serveUpload 上传的共享 HTTP 管道:流式读取 multipart 表单、档位与 TTL 解析、
// 调用上传服务并写响应;业务管道(格式校验/像素预算/净化/哈希/落库)在 upload.Service。
// @param userID 归属用户 id,由认证中间件解析后显式传入
// @param tokenID 来源 Token 记录 id,0 表示非 Token 途径(后台会话上传)
func (h *Handlers) serveUpload(w http.ResponseWriter, r *http.Request, userID string, tokenID int64) {
	set := h.cfg.Settings()
	form, err := readUploadForm(w, r, set.MaxUploadBytes())
	if err != nil {
		h.writeReadError(w, set, err)
		return
	}

	// 档位决策:单次表单字段 > 全局默认(在 API 层定死,Service 不做隐式回退)。
	policy := imaging.EXIFPolicy(form.exifPolicy)
	if policy == "" {
		policy = set.EXIFPolicy
	}

	ttl, ttlSet, ok := parseTTL(form.ttl)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.UploadTTLInvalid,
			"max", strconv.FormatInt(maxTTLSeconds, 10))
		return
	}

	h.runUpload(w, r, set, form.data, upload.Options{
		UserID:         userID,
		TokenID:        tokenID,
		FileName:       form.fileName,
		IdempotencyKey: r.Header.Get(idempotencyHeader),
		EXIFPolicy:     policy,
		TTL:            ttl,
		TTLSet:         ttlSet,
	})
}

// multipartOverhead 上传请求体在文件之外的余量:multipart 边界、各段头与 exif_policy / ttl 等短字段。
const multipartOverhead = 64 << 10

// maxFieldBytes 单个短字段(exif_policy / ttl)的字节上限。
const maxFieldBytes = 1 << 10

// 上传表单的读取错误,由 writeReadError 翻译成状态码。
var (
	errFileTooLarge = errors.New("publicapi: 上传文件超过单图上限")
	errFieldTooLong = errors.New("publicapi: 表单字段过长")
	errNoFile       = errors.New("publicapi: 缺少 file 文件段")
)

// uploadForm 从 multipart 请求体读出的上传表单。
type uploadForm struct {
	hasFile    bool   // 是否读到了带文件名的 file 段
	fileName   string // file 段的文件名
	data       []byte // file 段的完整字节
	exifPolicy string // exif_policy 字段;空表示未指定
	ttl        string // ttl 字段;空表示未指定
}

// readUploadForm 流式读取 multipart 上传表单:file 段直接读进内存,不落临时文件。
// @Description: 单图上限只约束 file 段本身;请求体整体以单图上限加 multipartOverhead 为界,挡住超大请求体。
// 只认第一个带文件名的 file 段与 exif_policy、ttl 两个短字段,其余段跳过不读。
// @param maxFile 单图字节上限
// @return 上传表单
// @return errFileTooLarge 或 *http.MaxBytesError(超限)、errNoFile、errFieldTooLong 或 multipart 格式错误
func readUploadForm(w http.ResponseWriter, r *http.Request, maxFile int64) (uploadForm, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFile+multipartOverhead)
	mr, err := r.MultipartReader()
	if err != nil {
		return uploadForm{}, err
	}
	var form uploadForm
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return uploadForm{}, err
		}
		if err := form.readPart(part, maxFile); err != nil {
			return uploadForm{}, err
		}
	}
	if !form.hasFile {
		return uploadForm{}, errNoFile
	}
	return form, nil
}

// readPart 按段名读入一段;不认识的段与第一个之后的 file 段不读,由下一次 NextPart 跳过。
// @param part 当前段
// @param maxFile 单图字节上限
// @return 超限或读取失败时的错误
func (f *uploadForm) readPart(part *multipart.Part, maxFile int64) error {
	var err error
	switch part.FormName() {
	case "file":
		if part.FileName() == "" || f.hasFile {
			return nil
		}
		f.fileName, f.hasFile = part.FileName(), true
		f.data, err = readLimited(part, maxFile, errFileTooLarge)
	case "exif_policy":
		f.exifPolicy, err = readField(part)
	case "ttl":
		f.ttl, err = readField(part)
	default:
		// 不认识的段不读,字节仍计入请求体上限。
	}
	return err
}

// readField 读一个短字段,超过 maxFieldBytes 即 errFieldTooLong。
// @param part 字段段
// @return 字段值
// @return 超限或读取失败时的错误
func readField(part *multipart.Part) (string, error) {
	v, err := readLimited(part, maxFieldBytes, errFieldTooLong)
	return string(v), err
}

// readLimited 读完 r,超过 limit 字节即返回 tooLarge。
// @param r 数据源
// @param limit 字节上限,恰好等于上限可通过
// @param tooLarge 超限时返回的错误
// @return 全部字节
// @return 超限或读取失败时的错误
func readLimited(r io.Reader, limit int64, tooLarge error) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, tooLarge
	}
	return data, nil
}

// runUpload 上传链路的公共尾段:调用上传服务、映射错误、写成功响应。
// @Description: multipart 上传、URL 转存与 S3 协议入口只在"字节从哪来"上不同,
// 拿到字节之后的编排、错误映射与响应形态必须一致,故收敛于此。
// @param set 本次请求生效的配置快照
// @param data 待上传的图片字节
// @param opts 上传选项,调用方按各自入口解析后填好
func (h *Handlers) runUpload(w http.ResponseWriter, r *http.Request, set config.Settings, data []byte,
	opts upload.Options) {
	res, err := h.cfg.Upload.Process(r.Context(), data, opts)
	if err != nil {
		h.writeUploadError(w, set, err)
		return
	}
	h.cfg.Logger.Info("上传成功", "id", res.ImageID, "file", opts.FileName, "size", len(data))
	writeUploaded(h, w, r, set, res.ImageID)
}

// writeUploaded 写上传成功的响应体:短 ID 与绝对访问地址。
// @param set 配置快照
// @param imageID 对外短 ID
func writeUploaded(h *Handlers, w http.ResponseWriter, r *http.Request, set config.Settings, imageID string) {
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, uploadResponse{
		ID:  imageID,
		URL: imageURL(r, set, imageID),
	})
}

// imageURL 拼图片的对外绝对地址。
// @Description: 配置了 base_url 一律用配置;未配置则按当前请求推导,保证响应里的 url 恒为绝对地址。
// @param set 配置快照
// @param imageID 对外短 ID
// @return 绝对访问地址
func imageURL(r *http.Request, set config.Settings, imageID string) string {
	base := set.BaseURL
	if base == "" {
		base = httpx.RequestBaseURL(r)
	}
	return base + "/i/" + imageID
}

// writeUploadError 把上传服务的业务错误映射为 HTTP 状态码与用户可读消息。
// @param set 本次请求生效的配置快照,错误文案里的阈值与闸值判定同源
func (h *Handlers) writeUploadError(w http.ResponseWriter, set config.Settings, err error) {
	switch {
	case errors.Is(err, upload.ErrUnsupportedType):
		httpx.WriteError(h.cfg.Logger, w, http.StatusUnsupportedMediaType, errcode.UploadUnsupportedType)
	case errors.Is(err, upload.ErrTooManyPixels):
		httpx.WriteError(h.cfg.Logger, w, http.StatusUnprocessableEntity, errcode.UploadTooManyPixels,
			"max", strconv.FormatInt(set.MaxPixelsMP, 10))
	case errors.Is(err, upload.ErrInvalidImage):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.UploadInvalidImage)
	case errors.Is(err, imaging.ErrUnknownPolicy):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.UploadEXIFPolicy)
	case errors.Is(err, upload.ErrQuotaExceeded):
		httpx.WriteError(h.cfg.Logger, w, http.StatusForbidden, errcode.UploadQuotaExceeded)
	case errors.Is(err, channel.ErrNoHealthyChannel), errors.Is(err, upload.ErrInsufficientReplicas):
		h.cfg.Logger.Error("渠道不可用,上传失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusServiceUnavailable, errcode.UploadUnavailable)
	default:
		h.cfg.Logger.Error("上传处理失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
	}
}

// writeReadError 把上传表单的读取错误写为用户可读响应。
// @Description: 文件或请求体超限 → 413 并给出大小上限;短字段过长与其余表单错误 → 400。
// @param set 本次请求生效的配置快照,错误文案里的上限与大小闸同源
// @param err readUploadForm 返回的错误
func (h *Handlers) writeReadError(w http.ResponseWriter, set config.Settings, err error) {
	var maxErr *http.MaxBytesError
	switch {
	case errors.Is(err, errFileTooLarge), errors.As(err, &maxErr):
		httpx.WriteError(h.cfg.Logger, w, http.StatusRequestEntityTooLarge, errcode.UploadTooLarge,
			"max", strconv.FormatInt(set.MaxUploadMB, 10))
	case errors.Is(err, errFieldTooLong):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.UploadFieldTooLong,
			"max", strconv.Itoa(maxFieldBytes))
	default:
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.UploadNoFile)
	}
}
