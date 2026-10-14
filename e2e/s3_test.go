//go:build e2e

// S3 协议上传(迭代 8)的 e2e 覆盖:用官方 AWS SDK 打真实二进制,
// 证明 PicGo/awscli 这类内置 S3 客户端确实能把图传进来、贴出的链接确实能访问。
package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	signer "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// s3Credential 后台返回的 S3 凭证与端点配置。
type s3Credential struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	URLPrefix       string `json:"url_prefix"`
}

// fetchS3Credential 经后台取指定 Token 的 S3 凭证。
func fetchS3Credential(t *testing.T, inst *instance, client *http.Client, csrf string, tokenID int64) s3Credential {
	t.Helper()
	resp := adminReq(t, client, csrf, http.MethodPost,
		fmt.Sprintf("%s/api/admin/tokens/%d/s3-credential", inst.baseURL, tokenID), "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("取 S3 凭证状态码 = %d, 期望 200; body=%s", resp.StatusCode, raw)
	}
	var cred s3Credential
	if err := json.NewDecoder(resp.Body).Decode(&cred); err != nil {
		t.Fatalf("解析 S3 凭证: %v", err)
	}
	return cred
}

// newS3Client 按后台给出的凭证造一个官方 S3 客户端(路径风格寻址)。
func newS3Client(t *testing.T, cred s3Credential) *s3.Client {
	t.Helper()
	return s3.New(s3.Options{
		BaseEndpoint: aws.String(cred.Endpoint),
		Region:       cred.Region,
		Credentials: credentials.NewStaticCredentialsProvider(
			cred.AccessKeyID, cred.SecretAccessKey, ""),
		UsePathStyle: true, // 自部署没有 bucket 子域名,一律路径风格
	})
}

// s3Env 一套备好 S3 凭证的测试环境。
type s3Env struct {
	inst    *instance
	client  *http.Client // 管理员会话
	csrf    string       // 会话的 CSRF 令牌
	tokenID int64        // 凭证对应的 Token 记录 id
	cred    s3Credential
}

// s3Setup 起一个实例并备好一枚 Token 的 S3 凭证。
func s3Setup(t *testing.T, settings map[string]any) s3Env {
	t.Helper()
	inst := startInstance(t, settings)
	client, csrf := adminSession(t, inst)
	tokenID, _ := issueToken(t, inst, client, csrf, "s3-client")
	return s3Env{
		inst: inst, client: client, csrf: csrf, tokenID: tokenID,
		cred: fetchS3Credential(t, inst, client, csrf, tokenID),
	}
}

// putWith 用给定凭证发一次 PutObject,返回错误(成功则为 nil)。
func putWith(t *testing.T, cred s3Credential, key string) error {
	t.Helper()
	_, err := newS3Client(t, cred).PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(cred.Bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(pngBytes(t)),
	})
	return err
}

// assertS3Denied 断言这次上传被以 403 + SignatureDoesNotMatch 拒绝。
// @Description: 只断言"出错"会把 500 也算作通过,凭证校验的回归价值就没了。
func assertS3Denied(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s 应被拒绝", what)
		return
	}
	var respErr *awshttp.ResponseError
	if !errors.As(err, &respErr) {
		t.Errorf("%s: 期望 HTTP 响应错误,得 %v", what, err)
		return
	}
	if respErr.HTTPStatusCode() != http.StatusForbidden {
		t.Errorf("%s: 状态码 = %d, 期望 403", what, respErr.HTTPStatusCode())
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && apiErr.ErrorCode() != "SignatureDoesNotMatch" {
		t.Errorf("%s: 错误码 = %q, 期望 SignatureDoesNotMatch", what, apiErr.ErrorCode())
	}
}

// objectPath 把凭证给出的 urlPrefix 与对象键拼成站内路径。
func objectPath(inst *instance, cred s3Credential, objectKey string) string {
	return strings.TrimPrefix(cred.URLPrefix, inst.baseURL) + "/" + objectKey
}

// TestS3PutObjectRoundTrip S3 协议闭环:官方 SDK 上传 → 客户端按 urlPrefix 拼的地址可访问 →
// 字节与原图一致。这是"能否接上 PicGo 生态"的实质验收。
func TestS3PutObjectRoundTrip(t *testing.T) {
	t.Parallel()
	env := s3Setup(t, nil)
	inst, cred := env.inst, env.cred

	payload := pngBytes(t)
	const objectKey = "2026/07/photo.png"
	out, err := newS3Client(t, cred).PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(cred.Bucket),
		Key:    aws.String(objectKey),
		Body:   bytes.NewReader(payload),
	})
	if err != nil {
		t.Fatalf("S3 上传失败: %v", err)
	}
	if out.ETag == nil || *out.ETag == "" {
		t.Error("S3 上传响应缺 ETag")
	}

	// 客户端贴出的链接就是 urlPrefix + / + key,必须直接可访问且字节一致。
	if got := fetchBytes(t, inst, objectPath(inst, cred, objectKey)); !bytes.Equal(got, payload) {
		t.Error("按对象键取回的字节与上传不一致")
	}
}

// TestS3PutObjectOverwrite 同键重传按 S3 语义覆盖:别名指向新图,旧图的短 ID 仍可访问
// (可能已被分享出去,不能因为一次覆盖就失效)。
func TestS3PutObjectOverwrite(t *testing.T) {
	t.Parallel()
	env := s3Setup(t, nil)
	inst, client, csrf, cred := env.inst, env.client, env.csrf, env.cred
	s3c := newS3Client(t, cred)

	const objectKey = "cover.png"
	first, second := pngBytes(t), jpegWithEXIF(t)
	put := func(payload []byte) {
		t.Helper()
		if _, err := s3c.PutObject(context.Background(), &s3.PutObjectInput{
			Bucket: aws.String(cred.Bucket),
			Key:    aws.String(objectKey),
			Body:   bytes.NewReader(payload),
		}); err != nil {
			t.Fatalf("S3 上传失败: %v", err)
		}
	}

	// 先传一张并记下它的短 ID,覆盖后要验证这个 ID 仍然可用。
	put(first)
	beforeIDs, _ := listImages(t, inst, client, csrf)
	if len(beforeIDs) != 1 {
		t.Fatalf("首次上传后图片数 = %d, 期望 1", len(beforeIDs))
	}
	oldID := ""
	for id := range beforeIDs {
		oldID = id
	}
	put(second)

	if got := fetchBytes(t, inst, objectPath(inst, cred, objectKey)); !bytes.Equal(got, second) {
		t.Error("同键覆盖后应取回后传的那张")
	}
	// 旧图只是让出别名,其短链必须仍然可用。
	if got := fetchBytes(t, inst, "/i/"+oldID); !bytes.Equal(got, first) {
		t.Error("被覆盖的旧图短链应仍可访问且内容不变")
	}
	images, total := listImages(t, inst, client, csrf)
	if total != 2 {
		t.Fatalf("图片总数 = %d, 期望 2(覆盖只改别名、不删旧图); ids=%v", total, images)
	}
}

// TestS3RejectsBadCredential 凭证不对一律 403:伪造 access key、错误密钥、Token 吊销后。
func TestS3RejectsBadCredential(t *testing.T) {
	t.Parallel()
	env := s3Setup(t, nil)
	cred := env.cred

	forgedKey := cred
	forgedKey.AccessKeyID = "imgy_forged1"
	assertS3Denied(t, putWith(t, forgedKey, "x.png"), "伪造 access key")

	wrongSecret := cred
	wrongSecret.SecretAccessKey = strings.Repeat("f", len(cred.SecretAccessKey))
	assertS3Denied(t, putWith(t, wrongSecret, "x.png"), "错误密钥")

	// 吊销后凭证立即失效(密钥派生自 Token,Token 没了签名就无从校验)。
	rev := adminReq(t, env.client, env.csrf, http.MethodPost,
		fmt.Sprintf("%s/api/admin/tokens/%d/revoke", env.inst.baseURL, env.tokenID), "")
	_ = rev.Body.Close()
	assertS3Denied(t, putWith(t, cred, "x.png"), "吊销后的 Token")
}

// TestS3UnderRateLimit 开启上传频控后 S3 入口仍可用:频控按认证主体计,
// 而 S3 面的主体要验签才产生——验签若排在闸门之后,整个协议面会一律 401。
func TestS3UnderRateLimit(t *testing.T) {
	t.Parallel()
	env := s3Setup(t, map[string]any{"upload_rate_per_min": 60})
	if err := putWith(t, env.cred, "rated.png"); err != nil {
		t.Fatalf("频控开启后 S3 上传失败: %v", err)
	}
}

// TestS3ChunkedUpload aws-chunked 分块正文:aws-cli v2 与新版 botocore 默认走这条路,
// 不解帧则带帧的字节会被当成"不是图片"。此处手工构帧,不依赖某个 SDK 版本的默认行为。
func TestS3ChunkedUpload(t *testing.T) {
	t.Parallel()
	env := s3Setup(t, nil)
	inst, cred := env.inst, env.cred

	payload := pngBytes(t)
	framed := fmt.Sprintf("%x\r\n%s\r\n0\r\n\r\n", len(payload), payload)
	const objectKey = "chunked.png"
	req, err := http.NewRequest(http.MethodPut,
		inst.baseURL+"/s3/"+cred.Bucket+"/"+objectKey, strings.NewReader(framed))
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	req.Header.Set("Content-Encoding", "aws-chunked")
	req.Header.Set("X-Amz-Decoded-Content-Length", strconv.Itoa(len(payload)))
	signS3Request(t, req, cred, "STREAMING-UNSIGNED-PAYLOAD-TRAILER")

	resp, err := newClient(t).Do(req)
	if err != nil {
		t.Fatalf("发送请求: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("分块上传状态码 = %d, 期望 200; body=%s", resp.StatusCode, body)
	}

	// 解帧必须还原出原始字节:帧头若混进内容,取回的就不是同一张图。
	if got := fetchBytes(t, inst, objectPath(inst, cred, objectKey)); !bytes.Equal(got, payload) {
		t.Error("分块上传后取回的字节与原图不一致")
	}
}

// signS3Request 用凭证给请求签名(SigV4),供手工构造的请求使用。
func signS3Request(t *testing.T, req *http.Request, cred s3Credential, payloadHash string) {
	t.Helper()
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	creds := aws.Credentials{AccessKeyID: cred.AccessKeyID, SecretAccessKey: cred.SecretAccessKey}
	sig := signer.NewSigner(func(o *signer.SignerOptions) { o.DisableURIPathEscaping = true })
	if err := sig.SignHTTP(context.Background(), creds, req,
		payloadHash, "s3", cred.Region, time.Now().UTC()); err != nil {
		t.Fatalf("签名失败: %v", err)
	}
}

// TestS3RejectsNonImage S3 入口与直传共用同一条净化管道:非图片内容同样被拒。
func TestS3RejectsNonImage(t *testing.T) {
	t.Parallel()
	env := s3Setup(t, nil)

	_, err := newS3Client(t, env.cred).PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(env.cred.Bucket),
		Key:    aws.String("evil.png"),
		Body:   strings.NewReader("<script>alert(1)</script>"),
	})
	if err == nil {
		t.Fatal("非图片内容应被拒")
	}
}

// s3Status 取 SDK 错误里的 HTTP 状态码;不是 HTTP 响应错误时为 0。
func s3Status(err error) int {
	var respErr *awshttp.ResponseError
	if errors.As(err, &respErr) {
		return respErr.HTTPStatusCode()
	}
	return 0
}

// TestS3HeadAndGetObject HEAD/GET 闭环:上传后 HEAD 给出长度、类型、内容哈希 ETag 与上传时刻;GET 字节一致、
// ETag 与 HEAD 相同;If-None-Match 命中 304;不存在的键 404。
func TestS3HeadAndGetObject(t *testing.T) {
	t.Parallel()
	env := s3Setup(t, nil)
	cred := env.cred
	s3c := newS3Client(t, cred)
	payload := pngBytes(t)
	const objectKey = "album/head.png"
	if _, err := s3c.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(cred.Bucket), Key: aws.String(objectKey), Body: bytes.NewReader(payload),
	}); err != nil {
		t.Fatalf("S3 上传失败: %v", err)
	}

	head, err := s3c.HeadObject(context.Background(), &s3.HeadObjectInput{
		Bucket: aws.String(cred.Bucket), Key: aws.String(objectKey),
	})
	if err != nil {
		t.Fatalf("HeadObject: %v", err)
	}
	sum := sha256.Sum256(payload)
	wantETag := `"` + hex.EncodeToString(sum[:]) + `"`
	if head.ContentLength == nil || *head.ContentLength != int64(len(payload)) {
		t.Errorf("HEAD Content-Length = %v, 期望 %d", head.ContentLength, len(payload))
	}
	if head.ContentType == nil || *head.ContentType != "image/png" {
		t.Errorf("HEAD Content-Type = %v, 期望 image/png", head.ContentType)
	}
	if head.ETag == nil || *head.ETag != wantETag {
		t.Errorf("HEAD ETag = %v, 期望 %s", head.ETag, wantETag)
	}
	if head.LastModified == nil || time.Since(*head.LastModified) > time.Hour {
		t.Errorf("HEAD Last-Modified = %v, 期望为刚才的上传时刻", head.LastModified)
	}

	got, err := s3c.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(cred.Bucket), Key: aws.String(objectKey),
	})
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	body, err := io.ReadAll(got.Body)
	_ = got.Body.Close()
	if err != nil || !bytes.Equal(body, payload) {
		t.Fatalf("GET 字节与上传不一致: err=%v", err)
	}
	if got.ETag == nil || *got.ETag != wantETag {
		t.Errorf("GET ETag = %v, 期望与 HEAD 一致 %s", got.ETag, wantETag)
	}

	_, err = s3c.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(cred.Bucket), Key: aws.String(objectKey), IfNoneMatch: head.ETag,
	})
	if s3Status(err) != http.StatusNotModified {
		t.Errorf("If-None-Match 命中状态码 = %d, 期望 304 (err=%v)", s3Status(err), err)
	}
	_, err = s3c.HeadObject(context.Background(), &s3.HeadObjectInput{
		Bucket: aws.String(cred.Bucket), Key: aws.String("album/missing.png"),
	})
	if s3Status(err) != http.StatusNotFound {
		t.Errorf("不存在的键 HEAD 状态码 = %d, 期望 404 (err=%v)", s3Status(err), err)
	}
}

// TestS3DeleteObject 删除:DELETE 后 HEAD 与公开对象键地址都 404、再 DELETE 仍成功;
// 他人的对象 DELETE 同样成功但不删;坏凭证一律 403。
func TestS3DeleteObject(t *testing.T) {
	t.Parallel()
	env := s3Setup(t, nil)
	inst, cred := env.inst, env.cred
	s3c := newS3Client(t, cred)
	const objectKey = "del.png"
	if _, err := s3c.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(cred.Bucket), Key: aws.String(objectKey), Body: bytes.NewReader(pngBytes(t)),
	}); err != nil {
		t.Fatalf("S3 上传失败: %v", err)
	}
	headStatus := func(c *s3.Client) int {
		t.Helper()
		_, err := c.HeadObject(context.Background(), &s3.HeadObjectInput{
			Bucket: aws.String(cred.Bucket), Key: aws.String(objectKey),
		})
		if err == nil {
			return http.StatusOK
		}
		return s3Status(err)
	}
	del := func(c *s3.Client) error {
		_, err := c.DeleteObject(context.Background(), &s3.DeleteObjectInput{
			Bucket: aws.String(cred.Bucket), Key: aws.String(objectKey),
		})
		return err
	}

	// 另一个用户拿自己的凭证删这个键:成功但不动管理员的对象。
	createUser(t, inst, env.client, env.csrf, "bob", "bob-secret-1", 0)
	bob, bobCSRF := loginUser(t, inst, "bob", "bob-secret-1")
	bobTokenID, _ := issueToken(t, inst, bob, bobCSRF, "bob-s3")
	bobS3 := newS3Client(t, fetchS3Credential(t, inst, bob, bobCSRF, bobTokenID))
	if err := del(bobS3); err != nil {
		t.Fatalf("他人删除应按不存在处理而成功: %v", err)
	}
	if code := headStatus(s3c); code != http.StatusOK {
		t.Fatalf("他人删除后对象 HEAD 状态码 = %d, 期望仍为 200", code)
	}

	if err := del(s3c); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	if code := headStatus(s3c); code != http.StatusNotFound {
		t.Errorf("删除后 HEAD 状态码 = %d, 期望 404", code)
	}
	pub := fetch(t, inst, objectPath(inst, cred, objectKey), "")
	pubCode := pub.StatusCode
	_ = pub.Body.Close()
	if pubCode != http.StatusNotFound {
		t.Errorf("删除后公开对象键地址状态码 = %d, 期望 404", pubCode)
	}
	if err := del(s3c); err != nil {
		t.Errorf("再次删除应幂等成功: %v", err)
	}

	forged := cred
	forged.AccessKeyID = "imgy_forged1"
	assertS3Denied(t, del(newS3Client(t, forged)), "伪造 access key 删除")
	// HEAD 没有响应体,客户端拿不到错误码,只能按状态码断言。
	_, err := newS3Client(t, forged).HeadObject(context.Background(), &s3.HeadObjectInput{
		Bucket: aws.String(cred.Bucket), Key: aws.String(objectKey),
	})
	if s3Status(err) != http.StatusForbidden {
		t.Errorf("伪造 access key HEAD 状态码 = %d, 期望 403 (err=%v)", s3Status(err), err)
	}
}
