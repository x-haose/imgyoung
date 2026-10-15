package s3api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/auth"
	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/upload"
	"github.com/x-haose/imgyoung/internal/http/middleware"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
)

// contentSHAHeader 客户端声明的正文哈希头;SigV4 用它替代正文本体参与签名。
const contentSHAHeader = "X-Amz-Content-Sha256"

// uploadScope 本协议面要求的 Token 授权范围。
const uploadScope = "upload"

// s3PathPrefix S3 协议面的路径前缀;桶名与对象键都在它之后。
const s3PathPrefix = "/s3/"

// maxObjectKeyBytes 对象键长度上限,与 S3 一致(1024 字节)。
const maxObjectKeyBytes = 1024

// ErrTooLargeBody 正文超出单图大小上限。
var ErrTooLargeBody = errors.New("s3api: 对象超过大小上限")

// 路径解析的错误哨兵。
var (
	// errNoObjectKey 请求路径里没有对象键部分。
	errNoObjectKey = errors.New("s3api: 缺少对象键")
	// errKeyTooLong 对象键超出长度上限。
	errKeyTooLong = errors.New("s3api: 对象键过长")
	// errInvalidObjectKey 对象键含控制字符或以斜杠开头。
	errInvalidObjectKey = errors.New("s3api: 对象键含非法字符")
)

// ObjectServer 按短 ID 输出对象字节与元数据(由 publicapi.Handlers 实现)。
type ObjectServer interface {
	// ServeObject 向已验签的客户端输出原图;错误只在写出任何响应之前返回。
	ServeObject(w http.ResponseWriter, r *http.Request, imageID string) error
}

// ImageDeleter 删除一条引用(由 lifecycle.Sweeper 实现)。
type ImageDeleter interface {
	// DeleteImage 删除引用,引用计数与级联与后台删除同一语义。
	DeleteImage(ctx context.Context, imageID string) error
}

// Config Handlers 的全部依赖与参数。
type Config struct {
	Auth     *auth.Service          // 认证服务:按 access key 取凭证、派生密钥
	Upload   *upload.Service        // 上传编排服务
	Queries  *sqlc.Queries          // 查询集:按签名主体与对象键定位引用
	Objects  ObjectServer           // 对象输出口,HeadObject 与 GetObject 用
	Images   ImageDeleter           // 引用删除口,DeleteObject 用
	Audit    *audit.Recorder        // 审计记录器,删除留痕
	Settings func() config.Settings // 运营配置快照读取口(逐请求取值,热生效)
	Logger   *slog.Logger           // 结构化日志
}

// Handlers S3 协议处理器集,自身无可变状态、并发安全。
type Handlers struct {
	cfg Config
}

// New 创建 S3 协议处理器集。
// @param cfg 依赖与参数,字段均必填
// @return 就绪的处理器集
func New(cfg Config) *Handlers {
	return &Handlers{cfg: cfg}
}

// errorResponse S3 风格的 XML 错误体;客户端按 Code 分支处理,文案仅供人读。
type errorResponse struct {
	XMLName xml.Name `xml:"Error"`
	Code    string   `xml:"Code"`
	Message string   `xml:"Message"`
	Key     string   `xml:"Key,omitempty"`
}

// Authenticate 把 SigV4 校验做成中间件,通过后将 Token 主体放入请求上下文。
// @Description: 上传频控按认证主体计,而 S3 面的主体要验签才产生——校验若留在 handler 内,
// 频控中间件永远拿不到主体,开启频控后整个协议面一律 401。故验签必须排在闸门之前。
// @param next 下游处理器
// @return 中间件处理器
func (h *Handlers) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cred, err := h.verifyRequest(r)
		if err != nil {
			h.writeError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(middleware.WithTokenPrincipal(r.Context(), cred.Principal)))
	})
}

// HandlePutObject 处理 S3 PutObject:读正文 → 走同一条上传管道。
// @Description: 路径形如 /s3/<bucket>/<key...>。bucket 名不做归属绑定——本服务没有桶概念,
// 客户端填什么都收,归属由凭证决定;对象键连同 bucket 之后的整段路径落库为访问别名。
// 须挂在 Authenticate 之后,取不到主体属路由装配错误。
func (h *Handlers) HandlePutObject(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.TokenPrincipalFrom(r.Context())
	if !ok {
		h.cfg.Logger.Error("S3 上传请求缺少认证主体,检查路由装配")
		h.writeStatus(w, r, http.StatusInternalServerError, "InternalError", "服务内部错误")
		return
	}
	objectKey, err := objectKeyFromPath(r.URL.EscapedPath())
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	set := h.cfg.Settings()
	data, err := readBody(w, r, set)
	if err != nil {
		h.writeBodyError(w, r, set, err)
		return
	}

	res, err := h.cfg.Upload.Process(r.Context(), data, upload.Options{
		UserID:     principal.UserID,
		TokenID:    principal.TokenID,
		FileName:   objectKey[strings.LastIndex(objectKey, "/")+1:],
		EXIFPolicy: set.EXIFPolicy,
		ObjectKey:  objectKey,
	})
	if err != nil {
		h.writeUploadError(w, r, set, err)
		return
	}

	sum := sha256.Sum256(data)
	h.cfg.Logger.Info("S3 上传成功", "id", res.ImageID, "object_key", objectKey, "size", len(data))
	// ETag 用内容哈希:S3 客户端普遍拿它做上传完整性校验。
	w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
	w.Header().Set("x-amz-meta-image-id", res.ImageID)
	w.WriteHeader(http.StatusOK)
}

// readBody 读取并还原对象正文:分块编码先解帧,再按声明的哈希校验。
// @Description: 大小闸放在解帧后按真实字节计——分块帧头会让原始流略大于真实内容,
// 拿带帧的长度去比上限会误判超限。
// @param set 配置快照,大小上限出自它
// @return 真实正文
// @return 超限、帧非法或哈希不符时的错误
func readBody(w http.ResponseWriter, r *http.Request, set config.Settings) ([]byte, error) {
	limit := set.MaxUploadBytes()
	if isAWSChunked(r) {
		// 帧结构本身占不了多少,给原始流留一倍余量,真正的闸在解帧后按内容长度判。
		return decodeAWSChunked(http.MaxBytesReader(w, r.Body, limit*2), limit)
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		return nil, err
	}
	// 正文哈希必须与签名时声明的一致,否则签名过了正文仍可被中途掉包。
	if err := verifyPayload(r, data); err != nil {
		return nil, err
	}
	return data, nil
}

// HandleHeadBucket 处理 HeadBucket:客户端上传前常先探桶是否可用。
// @Description: 本服务无桶概念,凭证有效即认为桶可用——回 404 会让不少客户端直接放弃上传。
// 鉴权由 Authenticate 中间件完成,能走到这里即已验签通过。
func (*Handlers) HandleHeadBucket(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// verifyRequest 校验 SigV4 签名并返回凭证。
// @Description: 顺序是"先看时间戳再验签"——过期请求不必付出派生密钥与重算签名的代价。
// @param r 收到的请求
// @return 凭证
// @return 签名或凭证错误
func (h *Handlers) verifyRequest(r *http.Request) (auth.S3Credential, error) {
	parts, err := parseAuthorization(r.Header.Get("Authorization"))
	if err != nil {
		return auth.S3Credential{}, err
	}
	if err := checkClockSkew(r, time.Now().UTC()); err != nil {
		return auth.S3Credential{}, err
	}
	cred, err := h.cfg.Auth.LookupS3Credential(r.Context(), parts.accessKeyID)
	if err != nil {
		return auth.S3Credential{}, err
	}
	if err := verify(r, parts, cred.SecretKey, r.Header.Get(contentSHAHeader)); err != nil {
		return auth.S3Credential{}, err
	}
	// 精确成员判定,与 Bearer 路径同口径:子串匹配会让 no-upload 之类的 scope 误命中。
	if !slices.Contains(strings.Split(cred.Principal.Scopes, ","), uploadScope) {
		return auth.S3Credential{}, auth.ErrTokenInvalid
	}
	h.cfg.Auth.MarkTokenUsed(cred.Principal.TokenID)
	return cred, nil
}

// verifyPayload 校验正文与签名时声明的哈希一致。
// @Description: 客户端声明 UNSIGNED-PAYLOAD 时正文本就不在签名保护内,跳过比对;
// 声明了具体哈希却对不上,说明正文与签名不是同一份内容。
// @param r 收到的请求
// @param data 实际读到的正文
// @return 不一致时返回 ErrSignatureMismatch
func verifyPayload(r *http.Request, data []byte) error {
	declared := r.Header.Get(contentSHAHeader)
	if declared == "" || declared == unsignedPayload {
		return nil
	}
	sum := sha256.Sum256(data)
	if !strings.EqualFold(declared, hex.EncodeToString(sum[:])) {
		return ErrSignatureMismatch
	}
	return nil
}

// objectKeyFromPath 从请求路径切出对象键(剥掉协议面前缀与桶名两段)。
// @param escapedPath 请求路径(保持转义形态),形如 /s3/<bucket>/<key...>
// @return 对象键
// @return 路径不含对象键、键过长或含非法字符时的错误
func objectKeyFromPath(escapedPath string) (string, error) {
	trimmed, ok := strings.CutPrefix(escapedPath, s3PathPrefix)
	if !ok {
		return "", errNoObjectKey
	}
	_, key, found := strings.Cut(trimmed, "/")
	if !found || key == "" {
		return "", errNoObjectKey
	}
	// S3 的对象键就是解码后的路径:客户端签名时用转义形态,落库与访问用解码形态。
	decoded, err := url.PathUnescape(key)
	if err != nil {
		return "", errNoObjectKey
	}
	if len(decoded) > maxObjectKeyBytes {
		return "", errKeyTooLong
	}
	// 控制字符会原样进日志与对外 URL;前导斜杠让同一个键有两种写法,别名不再唯一。
	if strings.HasPrefix(decoded, "/") || strings.ContainsFunc(decoded, isControlRune) {
		return "", errInvalidObjectKey
	}
	return decoded, nil
}

// isControlRune 判定是否为控制字符(含 DEL)。
// @param r 待判字符
// @return 是控制字符为 true
func isControlRune(r rune) bool {
	return r < 0x20 || r == 0x7F
}

// writeBodyError 把正文读取阶段的错误翻译成 S3 错误码。
// @param set 配置快照,阈值文案与闸值同源
// @param err 读取阶段的原始错误
func (h *Handlers) writeBodyError(w http.ResponseWriter, r *http.Request, set config.Settings, err error) {
	var maxErr *http.MaxBytesError
	switch {
	case errors.As(err, &maxErr), errors.Is(err, ErrTooLargeBody):
		h.writeStatus(w, r, http.StatusBadRequest, "EntityTooLarge",
			fmt.Sprintf("对象超过大小上限(%d MB)", set.MaxUploadMB))
	case errors.Is(err, ErrSignatureMismatch):
		h.writeError(w, r, err)
	case errors.Is(err, errMalformedChunk):
		h.writeStatus(w, r, http.StatusBadRequest, "IncompleteBody", "分块正文格式非法")
	default:
		h.writeStatus(w, r, http.StatusBadRequest, "IncompleteBody", "读取对象内容失败")
	}
}

// writeUploadError 把上传管道的业务错误翻译成 S3 错误码。
// @param set 配置快照,阈值文案与闸值同源
// @param err 上传阶段的原始错误
func (h *Handlers) writeUploadError(w http.ResponseWriter, r *http.Request, set config.Settings, err error) {
	switch {
	case errors.Is(err, upload.ErrUnsupportedType), errors.Is(err, upload.ErrInvalidImage),
		errors.Is(err, imaging.ErrUnknownPolicy):
		h.writeStatus(w, r, http.StatusBadRequest, "InvalidRequest", "仅支持图片文件(jpeg/png/gif/webp)")
	case errors.Is(err, upload.ErrTooManyPixels):
		h.writeStatus(w, r, http.StatusBadRequest, "InvalidRequest",
			fmt.Sprintf("图片分辨率超出限制(像素预算 %d 百万像素)", set.MaxPixelsMP))
	case errors.Is(err, upload.ErrQuotaExceeded):
		h.writeStatus(w, r, http.StatusForbidden, "QuotaExceeded", "存储配额已满")
	case errors.Is(err, channel.ErrNoHealthyChannel), errors.Is(err, upload.ErrInsufficientReplicas):
		// 渠道故障可重试:与 HTTP 上传接口同为 503,S3 客户端据此退避重试而非当作服务端缺陷
		h.cfg.Logger.Error("渠道不可用,S3 上传失败", "err", err)
		h.writeStatus(w, r, http.StatusServiceUnavailable, "ServiceUnavailable", "存储渠道暂不可用,请稍后重试")
	default:
		h.cfg.Logger.Error("S3 上传处理失败", "err", err)
		h.writeStatus(w, r, http.StatusInternalServerError, "InternalError", "服务内部错误")
	}
}

// writeError 把认证与路径错误翻译成 S3 错误码。
// @Description: 凭证不存在与签名不符一律回同一个码,不告诉调用方"这个 access key 存在"。
// @param err 原始错误
func (h *Handlers) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrMalformedAuth):
		h.writeStatus(w, r, http.StatusBadRequest, "AuthorizationHeaderMalformed", "Authorization 头格式非法")
	case errors.Is(err, ErrUnsupportedAlgorithm):
		h.writeStatus(w, r, http.StatusBadRequest, "InvalidRequest", "仅支持 AWS4-HMAC-SHA256 签名")
	case errors.Is(err, ErrRequestExpired):
		h.writeStatus(w, r, http.StatusForbidden, "RequestTimeTooSkewed", "请求时间戳与服务器相差过大")
	case errors.Is(err, ErrSignatureMismatch), errors.Is(err, auth.ErrTokenInvalid):
		h.writeStatus(w, r, http.StatusForbidden, "SignatureDoesNotMatch", "凭证无效或签名不匹配")
	case errors.Is(err, errKeyTooLong):
		h.writeStatus(w, r, http.StatusBadRequest, "KeyTooLongError", "对象键过长")
	case errors.Is(err, errInvalidObjectKey):
		h.writeStatus(w, r, http.StatusBadRequest, "InvalidArgument", "对象键含非法字符")
	case errors.Is(err, errNoObjectKey):
		h.writeStatus(w, r, http.StatusBadRequest, "InvalidRequest", "缺少对象键")
	default:
		h.cfg.Logger.Error("S3 请求处理失败", "err", err)
		h.writeStatus(w, r, http.StatusInternalServerError, "InternalError", "服务内部错误")
	}
}

// writeStatus 写一个 S3 风格的 XML 错误响应;HEAD 请求只写状态码,HTTP 不允许其携带响应体。
// @param status HTTP 状态码
// @param code S3 错误码
// @param message 人读文案
func (h *Handlers) writeStatus(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	body := errorResponse{Code: code, Message: message, Key: strings.TrimPrefix(r.URL.Path, "/")}
	if err := xml.NewEncoder(w).Encode(body); err != nil {
		h.cfg.Logger.Debug("写 S3 错误响应失败", "err", err)
	}
}
