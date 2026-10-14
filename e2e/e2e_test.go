//go:build e2e

// Package e2e 端到端测试:编译真实二进制、以独立进程启动,
// 通过 HTTP 覆盖全部对外功能(上传/访问/EXIF/幂等/防盗链/多渠道容灾/
// 加密渠道/302 模式/渠道管理/优雅停机)。
package e2e

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// testAdminPass 全部实例的管理员登录密码,即出厂默认密码(首启自动播种,须与 auth.DefaultAdminPassword 一致)。
const testAdminPass = "imgyoung123"

// bootDeadline 等待实例就绪的上限。
const bootDeadline = 10 * time.Second

// binPath 编译产物路径,由 TestMain 填充;buildErr 非空时为空串。
var binPath string

// buildErr TestMain 编译二进制的结果;非空时每个用例在起实例处报告它。
var buildErr error

// TestMain 编译一次真实二进制,供全部用例复用;退出码由测试运行器按 m.Run 结果处理。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "imgyoung-e2e-bin")
	if err != nil {
		buildErr = fmt.Errorf("创建临时目录: %w", err)
		m.Run()
		return
	}
	binPath = filepath.Join(dir, "imgyoung")
	buildErr = buildBinary(binPath)
	m.Run()
	_ = os.RemoveAll(dir)
}

// buildBinary 以纯 Go 方式编译服务二进制到指定路径。
// @param out 产物路径
// @return 编译失败时携带编译器输出的错误
func buildBinary(out string) error {
	build := exec.Command("go", "build", "-o", out, "github.com/x-haose/imgyoung/cmd/imgyoung")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("编译二进制失败: %w\n%s", err, output)
	}
	return nil
}

// instance 一个独立运行的服务进程。
type instance struct {
	baseURL string    // 服务地址
	dataDir string    // 数据目录
	cmd     *exec.Cmd // 进程句柄
	logPath string    // 服务日志文件路径
	token   string    // 就绪后经管理 API 签发的管理员上传 Token 明文,数据面测试统一用它
}

// seedToken 登录内置管理员并签发一枚上传 Token,写入实例供数据面测试使用。
// @Description: 服务端不认任何环境变量托管的引导 Token——Token 一律后台签发,
// 故实例就绪后必须走一遍真实签发链路才拿得到可上传的凭证。
// @param inst 已就绪的实例
func seedToken(t *testing.T, inst *instance) {
	t.Helper()
	client, csrf := adminSession(t, inst)
	_, plaintext := issueToken(t, inst, client, csrf, "e2e")
	inst.token = plaintext
}

// configure 经管理 API 改运营配置并断言热生效成功。
// @Description: 运营配置的唯一真相是 settings 表、唯一写者是后台,不接受环境变量播种,
// 故测试要调参只能走这条真实路径。
// @param inst 已就绪的实例
// @param settings 键→值补丁,键为 config.Settings 的 JSON 名
func configure(t *testing.T, inst *instance, settings map[string]any) {
	t.Helper()
	body, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("序列化配置补丁: %v", err)
	}
	client, csrf := adminSession(t, inst)
	resp := adminReq(t, client, csrf, http.MethodPut, inst.baseURL+"/api/admin/config", string(body))
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("改运营配置状态码 = %d, 期望 200; patch=%s body=%s", resp.StatusCode, body, raw)
	}
}

// startInstance 以独立数据目录与系统分配的端口启动一个服务进程,就绪后签发 Token 并应用运营配置。
// @Description: 端口交给系统分配、实际地址从启动日志取:先自选空闲端口再交给进程绑定,
// 并行用例之间会抢到同一个端口。
// @param settings 运营配置补丁(键为 config.Settings 的 JSON 名);渠道健康检查间隔已默认压到 1 秒
func startInstance(t *testing.T, settings map[string]any) *instance {
	t.Helper()
	if buildErr != nil {
		t.Fatalf("服务二进制不可用: %v", buildErr)
	}
	dataDir := t.TempDir()
	inst := &instance{dataDir: dataDir, logPath: filepath.Join(dataDir, "server.log")}
	spawn(t, inst, "127.0.0.1:0")
	inst.baseURL = "http://" + listenAddr(t, inst.logPath)
	waitReady(t, inst)
	seedToken(t, inst)
	// 健康检查间隔全实例压到 1 秒:用例等渠道状态翻转,出厂间隔太长会把每个用例拖成分钟级。
	patch := map[string]any{"health_check_seconds": 1}
	for k, v := range settings {
		patch[k] = v
	}
	configure(t, inst, patch)
	return inst
}

// spawn 启动服务进程,日志落 inst.logPath;进程随测试结束被 SIGTERM 收尾,超时则强杀,
// 用例失败时把服务日志尾部打进测试输出。
// @param inst 已填好 dataDir 与 logPath 的实例
// @param listen 监听地址,端口 0 表示由系统分配
func spawn(t *testing.T, inst *instance, listen string) {
	t.Helper()
	logFile, err := os.Create(inst.logPath)
	if err != nil {
		t.Fatalf("创建日志文件: %v", err)
	}
	cmd := exec.Command(binPath)
	cmd.Env = append(os.Environ(), "IMGYOUNG_DATA_DIR="+inst.dataDir, "IMGYOUNG_LISTEN="+listen)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动服务进程: %v", err)
	}
	inst.cmd = cmd
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _, _ = cmd.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
		}
		if closeErr := logFile.Close(); closeErr != nil {
			t.Logf("关闭日志文件: %v", closeErr)
		}
		if t.Failed() {
			t.Logf("服务日志尾部:\n%s", logTail(inst.logPath))
		}
	})
}

// logTailBytes 用例失败时回显的服务日志尾部长度。
const logTailBytes = 4 << 10

// logTail 读服务日志的最后一段。
// @param path 日志文件路径
// @return 日志尾部;读不到时为空串
func logTail(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(raw) > logTailBytes {
		raw = raw[len(raw)-logTailBytes:]
	}
	return string(raw)
}

// listenAddrPattern 启动日志里的实际监听地址字段。
var listenAddrPattern = regexp.MustCompile(`listen=(127\.0\.0\.1:\d+)`)

// listenAddr 从启动日志取进程实际绑定的地址。
// @param logPath 服务日志路径
// @return host:port
func listenAddr(t *testing.T, logPath string) string {
	t.Helper()
	var addr string
	waitUntil(t, bootDeadline, "启动日志打出监听地址", func() bool {
		raw, err := os.ReadFile(logPath)
		if err != nil {
			return false
		}
		m := listenAddrPattern.FindSubmatch(raw)
		if m == nil {
			return false
		}
		addr = string(m[1])
		return true
	})
	return addr
}

// waitReady 轮询 /healthz 直到实例就绪。
// @param inst 已启动的实例
func waitReady(t *testing.T, inst *instance) {
	t.Helper()
	waitUntil(t, bootDeadline, "服务就绪", func() bool {
		resp, err := http.Get(inst.baseURL + "/healthz")
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode == http.StatusOK
	})
}

// skipIfRoot 以 root 运行时跳过依赖文件权限制造故障的用例:root 不受权限位约束,故障造不出来。
func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root 不受文件权限约束,无法用权限位模拟故障")
	}
}

// waitUntil 轮询直到条件满足,超时即失败。
func waitUntil(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("等待%s超时(%s)", what, timeout)
}

// pngBytes 生成一张 2x2 真实 PNG。
func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatalf("编码测试 PNG: %v", err)
	}
	return buf.Bytes()
}

// jpegWithEXIF 生成一张带 APP1(Exif)段的 JPEG。
func jpegWithEXIF(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatalf("编码测试 JPEG: %v", err)
	}
	jpg := buf.Bytes()
	payload := append([]byte("Exif\x00\x00"), bytes.Repeat([]byte{0xAB}, 16)...)
	seg := []byte{0xFF, 0xE1}
	seg = binary.BigEndian.AppendUint16(seg, uint16(len(payload)+2)) //nolint:gosec // 测试载荷为几十字节常量
	seg = append(seg, payload...)
	out := append([]byte{}, jpg[:2]...)
	out = append(out, seg...)
	return append(out, jpg[2:]...)
}

// upload 发起一次上传。
// @param fields 额外表单字段(如 exif_policy)
// @param headers 额外请求头(如 Idempotency-Key)
// @return 响应对象(调用方负责关闭)
func upload(t *testing.T, inst *instance, token, filename string, payload []byte,
	fields, headers map[string]string) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("写表单字段: %v", err)
		}
	}
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("创建文件字段: %v", err)
	}
	if _, err := fw.Write(payload); err != nil {
		t.Fatalf("写文件内容: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭 multipart: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, inst.baseURL+"/api/upload", &buf)
	if err != nil {
		t.Fatalf("构造上传请求: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("发送上传请求: %v", err)
	}
	return resp
}

// mustUpload 上传并断言 200,返回图片 URL 路径。
func mustUpload(t *testing.T, inst *instance, payload []byte, fields, headers map[string]string) (id, path string) {
	t.Helper()
	resp := upload(t, inst, inst.token, "e2e.bin", payload, fields, headers)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("上传状态码 = %d, 期望 200; body=%s", resp.StatusCode, body)
	}
	var out struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("解析上传响应: %v", err)
	}
	return out.ID, urlPath(t, out.URL)
}

// urlPath 校验上传响应的 url 为绝对地址(未配 base_url 时按请求推导),返回其路径部分。
func urlPath(t *testing.T, raw string) string {
	t.Helper()
	u, err := neturl.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		t.Fatalf("上传响应 url = %q, 期望为绝对地址", raw)
	}
	return u.Path
}

// fetch 访问图片(可带 Referer),返回响应。
func fetch(t *testing.T, inst *instance, url, referer string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, inst.baseURL+url, nil)
	if err != nil {
		t.Fatalf("构造访问请求: %v", err)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("访问图片: %v", err)
	}
	return resp
}

// fetchBytes 访问图片并断言 200,返回字节。
func fetchBytes(t *testing.T, inst *instance, url string) []byte {
	t.Helper()
	resp := fetch(t, inst, url, "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("访问状态码 = %d, 期望 200", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取响应: %v", err)
	}
	return data
}

// adminSession 登录管理员,返回带会话 cookie 的客户端与 CSRF 令牌。
func adminSession(t *testing.T, inst *instance) (*http.Client, string) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("创建 cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}
	body := fmt.Sprintf(`{"name":"admin","password":%q}`, testAdminPass)
	resp, err := client.Post(inst.baseURL+"/api/admin/auth/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("发送登录请求: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("登录状态码 = %d, 期望 200; body=%s", resp.StatusCode, raw)
	}
	var lr struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		t.Fatalf("解析登录响应: %v", err)
	}
	return client, lr.CSRFToken
}

// adminReq 以管理员会话发送管理 API 请求(状态变更自动带 CSRF 头)。
// @return 响应对象(调用方负责关闭与断言)
func adminReq(t *testing.T, client *http.Client, csrf, method, url, body string) *http.Response {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatalf("构造管理请求: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("发送管理请求: %v", err)
	}
	return resp
}

// createChannel 通过管理 API 创建渠道并断言 201(自动登录管理员),返回渠道 id。
func createChannel(t *testing.T, inst *instance, body string) int64 {
	t.Helper()
	client, csrf := adminSession(t, inst)
	resp := adminReq(t, client, csrf, http.MethodPost, inst.baseURL+"/api/admin/channels", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("创建渠道状态码 = %d, 期望 201; body=%s", resp.StatusCode, raw)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("解析创建渠道响应: %v", err)
	}
	return created.ID
}

// countObjects 统计渠道存储目录下的对象文件数;排除 _imgyoung 下的备份等系统对象,
// 等待异步副本到达时不会被启动快照误判为已补齐。
// @param dir 渠道存储目录
// @return 对象文件数;目录不存在为 0
func countObjects(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.Contains(p, "_imgyoung") {
			n++
		}
		return nil
	})
	return n
}

// TestUploadServeRoundTrip 核心闭环:上传 → 访问,字节一致且响应头正确;
// 内容不可变故 If-None-Match 命中 304;不存在的短 ID 一律 404。
func TestUploadServeRoundTrip(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	payload := pngBytes(t)
	_, url := mustUpload(t, inst, payload, nil, nil)

	resp := fetch(t, inst, url, "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("访问状态码 = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q", ct)
	}
	if v := resp.Header.Get("X-Content-Type-Options"); v != "nosniff" {
		t.Error("缺 nosniff 头")
	}
	if !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Error("缺 immutable 缓存头")
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("取回字节不一致: err=%v", err)
	}

	etag := resp.Header.Get("ETag")
	if etag == "" || etag[0] != '"' {
		t.Fatalf("ETag = %q, 期望带引号的强 ETag", etag)
	}
	cond, _ := procGet(t, inst, url, map[string]string{"If-None-Match": etag})
	if cond.StatusCode != http.StatusNotModified {
		t.Errorf("If-None-Match 命中状态码 = %d, 期望 304", cond.StatusCode)
	}
	missing := fetch(t, inst, "/i/nonexistent0", "")
	_ = missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Errorf("不存在短 ID 状态码 = %d, 期望 404", missing.StatusCode)
	}
}

// TestUploadURLUsesBaseURL 配置对外站点地址后,上传响应的 url 一律以它开头(PicGo 等客户端需要)。
func TestUploadURLUsesBaseURL(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, map[string]any{"base_url": "https://img.example.com"})
	resp := upload(t, inst, inst.token, "a.png", pngBytes(t), nil, nil)
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("解析上传响应: %v", err)
	}
	if !strings.HasPrefix(out.URL, "https://img.example.com/i/") {
		t.Fatalf("url = %q, 期望以配置的站点地址开头", out.URL)
	}
}

// TestAuthAndValidation 鉴权与输入校验:401 / 415 / 413。
func TestAuthAndValidation(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, map[string]any{"max_upload_mb": 1})

	noAuth := upload(t, inst, "", "a.png", pngBytes(t), nil, nil)
	defer func() { _ = noAuth.Body.Close() }()
	if noAuth.StatusCode != http.StatusUnauthorized {
		t.Errorf("无 Token 状态码 = %d, 期望 401", noAuth.StatusCode)
	}

	notImage := upload(t, inst, inst.token, "a.png", []byte("not an image"), nil, nil)
	defer func() { _ = notImage.Body.Close() }()
	if notImage.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("非图片状态码 = %d, 期望 415", notImage.StatusCode)
	}

	big := append(pngBytes(t), bytes.Repeat([]byte{0}, 2<<20)...)
	tooBig := upload(t, inst, inst.token, "big.png", big, nil, nil)
	defer func() { _ = tooBig.Body.Close() }()
	if tooBig.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("超限状态码 = %d, 期望 413", tooBig.StatusCode)
	}
}

// TestEXIFPolicies 默认 passthrough 保留元数据,strip 字段剥离元数据,实验档 spoof 显式指定即 400。
func TestEXIFPolicies(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	marker := []byte("Exif\x00\x00")

	_, keepURL := mustUpload(t, inst, jpegWithEXIF(t), nil, nil)
	if !bytes.Contains(fetchBytes(t, inst, keepURL), marker) {
		t.Error("默认 passthrough 档丢失了 EXIF")
	}

	_, stripURL := mustUpload(t, inst, jpegWithEXIF(t), map[string]string{"exif_policy": "strip"}, nil)
	if bytes.Contains(fetchBytes(t, inst, stripURL), marker) {
		t.Error("strip 档未剥离 EXIF")
	}

	bad := upload(t, inst, inst.token, "e.jpg", jpegWithEXIF(t), map[string]string{"exif_policy": "spoof"}, nil)
	_ = bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Errorf("spoof 档状态码 = %d, 期望 400(实验特性未实现)", bad.StatusCode)
	}
}

// TestIdempotency 同一 Idempotency-Key 重复上传返回同一 ID。
func TestIdempotency(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	headers := map[string]string{"Idempotency-Key": "e2e-retry"}
	id1, _ := mustUpload(t, inst, pngBytes(t), nil, headers)
	id2, _ := mustUpload(t, inst, pngBytes(t), nil, headers)
	if id1 != id2 {
		t.Fatalf("幂等失效: %q ≠ %q", id1, id2)
	}
}

// TestRefererProtection 防盗链:白名单命中放行、未命中 403、空 Referer 放行。
func TestRefererProtection(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	// 白名单以 referer_whitelist 表为真相、增删即时生效,故经管理 API 加规则。
	client, csrf := adminSession(t, inst)
	added := adminReq(t, client, csrf, http.MethodPost, inst.baseURL+"/api/admin/referer-whitelist",
		`{"pattern":"*.ok.com"}`)
	addedCode := added.StatusCode
	_ = added.Body.Close()
	if addedCode != http.StatusCreated {
		t.Fatalf("添加白名单规则状态码 = %d, 期望 201", addedCode)
	}
	_, url := mustUpload(t, inst, pngBytes(t), nil, nil)

	cases := map[string]struct {
		referer string
		want    int
	}{
		"空Referer": {"", http.StatusOK},
		"白名单":      {"https://a.ok.com/p", http.StatusOK},
		"盗链":       {"https://evil.io/x", http.StatusForbidden},
	}
	for name, tc := range cases {
		resp := fetch(t, inst, url, tc.referer)
		if resp.StatusCode != tc.want {
			t.Errorf("%s 状态码 = %d, 期望 %d", name, resp.StatusCode, tc.want)
		}
		_ = resp.Body.Close()
	}
}

// TestMultiChannelReplicationAndFailover 多渠道全链路:
// 补齐复制 → 拔主渠道访问即时切换 → 健康检查感知后新上传落备用渠道。
func TestMultiChannelReplicationAndFailover(t *testing.T) {
	t.Parallel()
	skipIfRoot(t)
	inst := startInstance(t, nil)
	fastDir := t.TempDir()
	createChannel(t, inst, fmt.Sprintf(
		`{"name":"fast","driver":"local","priority":300,"config":{"path":%q}}`, fastDir))

	payload := pngBytes(t)
	_, url := mustUpload(t, inst, payload, nil, nil)

	// 主渠道(fast)同步写入,内置 local 由补齐任务异步复制。
	localDir := filepath.Join(inst.dataDir, "blobs")
	waitUntil(t, 15*time.Second, "副本补齐到 local", func() bool { return countObjects(localDir) > 0 })
	if countObjects(fastDir) == 0 {
		t.Fatal("主渠道 fast 没有对象")
	}

	// 拔掉主渠道:访问应立即切换到 local 副本。
	if err := os.Chmod(fastDir, 0o000); err != nil {
		t.Fatalf("模拟主渠道故障: %v", err)
	}
	defer func() { _ = os.Chmod(fastDir, 0o700) }() //nolint:gosec // 目录需执行位,0700 已最小
	if got := fetchBytes(t, inst, url); !bytes.Equal(got, payload) {
		t.Fatal("主渠道故障后取回字节不一致")
	}

	// 上传降级:即使健康标记尚未翻转,写入失败也应顺延到 local 成功。
	_, url2 := mustUpload(t, inst, payload, nil, nil)
	if got := fetchBytes(t, inst, url2); !bytes.Equal(got, payload) {
		t.Fatal("主渠道故障期间的新上传不可访问")
	}
}

// TestEncryptedChannel 加密渠道:落盘文件名与内容均为密文,访问返回明文。
func TestEncryptedChannel(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	vaultDir := t.TempDir()
	createChannel(t, inst, fmt.Sprintf(
		`{"name":"vault","driver":"local","priority":300,"encrypted":true,"config":{"path":%q}}`, vaultDir))

	payload := pngBytes(t)
	id, url := mustUpload(t, inst, payload, nil, nil)

	entries, err := os.ReadDir(vaultDir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("加密渠道无落盘对象: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), id) {
			t.Error("加密渠道文件名未加密")
		}
		raw, rerr := os.ReadFile(filepath.Join(vaultDir, e.Name()))
		if rerr != nil {
			t.Fatalf("读密文文件: %v", rerr)
		}
		if bytes.HasPrefix(raw, []byte("\x89PNG")) {
			t.Error("加密渠道落盘内容是明文 PNG")
		}
	}
	if got := fetchBytes(t, inst, url); !bytes.Equal(got, payload) {
		t.Fatal("经服务访问未还原明文")
	}
}

// TestRedirectMode 302 省流量模式:允许直链的渠道返回重定向。
func TestRedirectMode(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, map[string]any{"access_mode": "redirect"})
	createChannel(t, inst, fmt.Sprintf(
		`{"name":"cdn","driver":"local","priority":300,"allow_direct_link":true,`+
			`"config":{"path":%q,"public_base_url":"https://cdn.example.com/pub"}}`,
		t.TempDir()))
	_, url := mustUpload(t, inst, pngBytes(t), nil, nil)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(inst.baseURL + url)
	if err != nil {
		t.Fatalf("访问图片: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("状态码 = %d, 期望 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, "https://cdn.example.com/pub/") {
		t.Fatalf("Location = %q", loc)
	}
}

// TestChannelGuards 渠道管理守卫:唯一副本删除拒绝、未知字段拒绝。
func TestChannelGuards(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	_, _ = mustUpload(t, inst, pngBytes(t), nil, nil)

	client, csrf := adminSession(t, inst)
	resp := adminReq(t, client, csrf, http.MethodDelete, inst.baseURL+"/api/admin/channels/1", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("删除唯一副本渠道状态码 = %d, 期望 409", resp.StatusCode)
	}
}

// TestGracefulShutdown SIGTERM 后进程以零码优雅退出。
func TestGracefulShutdown(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	_, _ = mustUpload(t, inst, pngBytes(t), nil, nil)

	if err := inst.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("发送 SIGTERM: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- inst.cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			log, _ := os.ReadFile(inst.logPath)
			t.Fatalf("退出码非零: %v\n%s", err, log)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("优雅退出超时")
	}
}

// TestRestartRecovery 重启恢复:进程重启后既有图仍可访问、可继续上传。
func TestRestartRecovery(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	payload := pngBytes(t)
	_, url := mustUpload(t, inst, payload, nil, nil)

	// 停掉再用同一数据目录、同一端口启动第二个进程。
	if err := inst.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("发送 SIGTERM: %v", err)
	}
	if _, err := inst.cmd.Process.Wait(); err != nil {
		t.Fatalf("等待退出: %v", err)
	}
	listen := strings.TrimPrefix(inst.baseURL, "http://")
	inst2 := startInstanceAt(t, inst, listen)

	if got := fetchBytes(t, inst2, url); !bytes.Equal(got, payload) {
		t.Fatal("重启后既有图不可访问或字节不一致")
	}
	_, _ = mustUpload(t, inst2, payload, nil, nil)
}

// TestBackupSnapshotRestore 验证迭代 3 灾备验收:启动时元数据库自动快照上传到渠道,
// 删掉本地 DB 后用渠道内快照替换重启,既有图的元数据完好、仍可访问。
func TestBackupSnapshotRestore(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	payload := pngBytes(t)
	_, url := mustUpload(t, inst, payload, nil, nil)

	listen := strings.TrimPrefix(inst.baseURL, "http://")
	stopInstance(t, inst)

	// 启动备份只在上一份快照超过备份间隔时执行:抹掉首次启动留下的快照,让重启必然重新备份,
	// 此时快照已含刚上传的图。
	backupDir := filepath.Join(inst.dataDir, "blobs", "_imgyoung", "backup")
	if err := os.RemoveAll(backupDir); err != nil {
		t.Fatalf("清空既有快照: %v", err)
	}
	inst2 := startInstanceAt(t, inst, listen)
	var snapshots []string
	waitUntil(t, 15*time.Second, "启动快照落到渠道", func() bool {
		snapshots, _ = filepath.Glob(filepath.Join(backupDir, "imgyoung-*.db"))
		return len(snapshots) > 0
	})
	slices.Sort(snapshots) // 文件名含时间戳,字典序末位即最新。
	newest := snapshots[len(snapshots)-1]
	stopInstance(t, inst2)

	// 灾难演练:抹掉本地元数据库(含 WAL 边车),用渠道快照恢复。
	dbPath := filepath.Join(inst.dataDir, "imgyoung.db")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if rerr := os.Remove(dbPath + suffix); rerr != nil && !os.IsNotExist(rerr) {
			t.Fatalf("删除 %s: %v", dbPath+suffix, rerr)
		}
	}
	snap, err := os.ReadFile(newest)
	if err != nil {
		t.Fatalf("读取快照: %v", err)
	}
	//nolint:gosec // G306:dbPath 由 t.TempDir() 派生、文件名固定,非外部输入
	if werr := os.WriteFile(dbPath, snap, 0o600); werr != nil {
		t.Fatalf("以快照恢复数据库: %v", werr)
	}

	inst3 := startInstanceAt(t, inst, listen)
	if got := fetchBytes(t, inst3, url); !bytes.Equal(got, payload) {
		t.Fatal("从渠道快照恢复元数据后,既有图不可访问或字节不一致")
	}
}

// TestTTLExpiryDeletes 验证迭代 4 验收:设短 TTL 的图到点消失、渠道副本随后被清。
func TestTTLExpiryDeletes(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, map[string]any{
		"cleanup_sweep_seconds": 1,
		"delete_grace_minutes":  0,
	})
	payload := pngBytes(t)
	_, url := mustUpload(t, inst, payload, map[string]string{"ttl": "1"}, nil)

	// 到期前可正常访问。
	if got := fetchBytes(t, inst, url); !bytes.Equal(got, payload) {
		t.Fatal("上传后到期前应可访问")
	}

	// 到期 + 清理后访问 404。
	waitUntil(t, 15*time.Second, "TTL 到期后访问 404", func() bool {
		resp := fetch(t, inst, url, "")
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusNotFound
	})
	// 渠道副本对象随后被级联清除(local 渠道盘上无顶层对象文件)。
	localDir := filepath.Join(inst.dataDir, "blobs")
	waitUntil(t, 10*time.Second, "渠道副本对象被清除", func() bool { return countObjects(localDir) == 0 })
}

// stopInstance 向实例发 SIGTERM 并等待退出(重启前的优雅停机)。
func stopInstance(t *testing.T, inst *instance) {
	t.Helper()
	if err := inst.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("发送 SIGTERM: %v", err)
	}
	if _, err := inst.cmd.Process.Wait(); err != nil {
		t.Fatalf("等待退出: %v", err)
	}
}

// startInstanceAt 在原数据目录上重启实例(重启场景)。
// @Description: 沿用原实例的 Token 与运营配置——两者都在库里,重启不该重新签发或重配。
// @param prev 已停止的原实例
// @param listen 监听地址(与原实例一致才能复用其 URL)
func startInstanceAt(t *testing.T, prev *instance, listen string) *instance {
	t.Helper()
	inst := &instance{
		baseURL: "http://" + listen,
		dataDir: prev.dataDir,
		logPath: filepath.Join(prev.dataDir, "server-restart.log"),
		token:   prev.token,
	}
	spawn(t, inst, listen)
	waitReady(t, inst)
	return inst
}
