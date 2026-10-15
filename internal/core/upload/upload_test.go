package upload_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/testenv"
	"github.com/x-haose/imgyoung/internal/core/upload"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
	storagedriver "github.com/x-haose/imgyoung/store/driver"
)

// testEnv 上传服务测试环境:共用装配之上的上传服务与内置 local 渠道的驱动。
type testEnv struct {
	*testenv.Env
	svc    *upload.Service      // 被测上传服务
	driver storagedriver.Driver // 内置 local 渠道的驱动,读回存储字节用
}

// newTestEnv 组装测试环境;mod 在出厂默认配置上覆写(nil 即取默认)。
func newTestEnv(t *testing.T, mod func(*config.Settings)) *testEnv {
	t.Helper()
	env := testenv.New(t)
	svc := upload.New(upload.Config{
		Conn: env.Conn, Queries: env.Queries, Channels: env.Manager,
		Settings: testenv.Settings(mod), Logger: env.Logger,
	})
	return &testEnv{Env: env, svc: svc, driver: env.Local(t).Driver}
}

// pngBytes 生成一张 2x2 的真实 PNG。
func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatalf("编码测试 PNG: %v", err)
	}
	return buf.Bytes()
}

// bigPNG 生成一张像素数超过一百万的真实 PNG,用于触发像素预算闸(尺寸取自图片头即拒)。
func bigPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1024, 1024))); err != nil {
		t.Fatalf("编码大 PNG: %v", err)
	}
	return buf.Bytes()
}

// jpegWithEXIF 生成一张带伪造 APP1(Exif)段的 JPEG。
func jpegWithEXIF(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatalf("编码测试 JPEG: %v", err)
	}
	jpg := buf.Bytes()
	payload := append([]byte("Exif\x00\x00"), bytes.Repeat([]byte{0xCD}, 16)...)
	seg := []byte{0xFF, 0xE1}
	seg = binary.BigEndian.AppendUint16(seg, uint16(len(payload)+2))
	seg = append(seg, payload...)

	return slices.Concat(jpg[:2], seg, jpg[2:])
}

// fetchStored 按 imageID 取出渠道内的实际存储字节。
func fetchStored(t *testing.T, env *testEnv, imageID string) []byte {
	t.Helper()
	row, err := env.Queries.GetImageBlob(t.Context(), liveImage(imageID))
	if err != nil {
		t.Fatalf("查询引用记录: %v", err)
	}
	rc, err := env.driver.Open(t.Context(), row.StorageKey)
	if err != nil {
		t.Fatalf("打开存储对象: %v", err)
	}
	data, err := io.ReadAll(rc)
	if cerr := rc.Close(); cerr != nil {
		t.Fatalf("关闭存储对象: %v", cerr)
	}
	if err != nil {
		t.Fatalf("读取存储对象: %v", err)
	}
	return data
}

// TestProcessStoresPNG 验证 PNG 上传:字节原样存储、记录完整、对象键含哈希前缀。
func TestProcessStoresPNG(t *testing.T) {
	env := newTestEnv(t, nil)
	payload := pngBytes(t)

	res, err := env.svc.Process(t.Context(), payload, upload.Options{
		UserID: sqlc.DefaultUserID, FileName: "a.png", EXIFPolicy: imaging.PolicyPassthrough,
	})
	if err != nil {
		t.Fatalf("Process 返回错误: %v", err)
	}
	if len(res.ImageID) != 10 {
		t.Fatalf("ImageID = %q, 期望 10 位短 ID", res.ImageID)
	}

	stored := fetchStored(t, env, res.ImageID)
	if !bytes.Equal(stored, payload) {
		t.Fatal("PNG 存储字节与上传不一致(PNG 不应被改动)")
	}

	row, err := env.Queries.GetImageBlob(t.Context(), liveImage(res.ImageID))
	if err != nil {
		t.Fatalf("查询引用记录: %v", err)
	}
	sum := sha256.Sum256(payload)
	wantPrefix := res.ImageID + "_" + hex.EncodeToString(sum[:4])
	if row.StorageKey != wantPrefix {
		t.Errorf("StorageKey = %q, 期望 %q(short_id + sha256 前 8 位)", row.StorageKey, wantPrefix)
	}
	if row.Mime != "image/png" {
		t.Errorf("Mime = %q, 期望 image/png", row.Mime)
	}
}

// TestProcessStripsJPEGMetadata 验证 JPEG 上传时 EXIF 被剥离,
// 且哈希基于净化后的字节(先净化后哈希)。
func TestProcessStripsJPEGMetadata(t *testing.T) {
	env := newTestEnv(t, nil)
	payload := jpegWithEXIF(t)

	res, err := env.svc.Process(t.Context(), payload, upload.Options{
		UserID:     sqlc.DefaultUserID,
		FileName:   "a.jpg",
		EXIFPolicy: imaging.PolicyStrip,
	})
	if err != nil {
		t.Fatalf("Process 返回错误: %v", err)
	}
	stored := fetchStored(t, env, res.ImageID)
	if bytes.Equal(stored, payload) {
		t.Fatal("存储字节与上传原样一致,EXIF 未被剥离")
	}
	if bytes.Contains(stored, []byte("Exif\x00\x00")) {
		t.Fatal("存储字节仍包含 Exif 段")
	}
	// 哈希必须等于净化后字节的哈希,而非原始字节
	row, err := env.Queries.GetImageBlob(t.Context(), liveImage(res.ImageID))
	if err != nil {
		t.Fatalf("查询引用记录: %v", err)
	}
	sum := sha256.Sum256(stored)
	if !strings.Contains(row.StorageKey, hex.EncodeToString(sum[:4])) {
		t.Fatal("对象键中的哈希前缀不是净化后字节的 SHA256")
	}
}

// TestProcessPassthroughKeepsEXIF 验证 passthrough 档位原样存储、不动元数据。
func TestProcessPassthroughKeepsEXIF(t *testing.T) {
	env := newTestEnv(t, nil)
	payload := jpegWithEXIF(t)

	res, err := env.svc.Process(t.Context(), payload, upload.Options{
		UserID:     sqlc.DefaultUserID,
		FileName:   "a.jpg",
		EXIFPolicy: imaging.PolicyPassthrough,
	})
	if err != nil {
		t.Fatalf("Process 返回错误: %v", err)
	}
	stored := fetchStored(t, env, res.ImageID)
	if !bytes.Equal(stored, payload) {
		t.Fatal("passthrough 档位下存储字节应与上传完全一致")
	}
}

// TestProcessRejectsUnknownPolicy 验证非法档位返回 ErrUnknownPolicy。
// @Description: 档位决策在调用方(API 层)完成,Service 只接受明确档位——
// 空值同样拒绝,未实现的 spoof 与乱填值一视同仁。
func TestProcessRejectsUnknownPolicy(t *testing.T) {
	env := newTestEnv(t, nil)
	for _, policy := range []imaging.EXIFPolicy{"", "spoof", "bogus"} {
		_, err := env.svc.Process(t.Context(), pngBytes(t), upload.Options{
			UserID:     sqlc.DefaultUserID,
			FileName:   "a.png",
			EXIFPolicy: policy,
		})
		if !errors.Is(err, imaging.ErrUnknownPolicy) {
			t.Fatalf("policy=%q 错误 = %v, 期望 ErrUnknownPolicy", policy, err)
		}
	}
}

// TestProcessMarksReplicaStates 验证多渠道写入语义:主渠道副本 done,其余渠道 pending。
func TestProcessMarksReplicaStates(t *testing.T) {
	env := newTestEnv(t, nil)
	primaryID, err := env.Manager.Create(t.Context(), channel.CreateParams{
		Name: "fast", Driver: "local", Config: map[string]string{"path": t.TempDir()}, Priority: 200,
	})
	if err != nil {
		t.Fatalf("创建渠道: %v", err)
	}

	_, replicas := env.uploadPNG(t)
	if len(replicas) != 2 {
		t.Fatalf("副本记录数 = %d, 期望 2(每渠道一条)", len(replicas))
	}
	states := map[int64]string{}
	for _, rep := range replicas {
		states[rep.ChannelID] = rep.State
	}
	if states[primaryID] != channel.StateDone {
		t.Errorf("主渠道状态 = %q, 期望 done(同步写入)", states[primaryID])
	}
	for chID, state := range states {
		if chID != primaryID && state != channel.StatePending {
			t.Errorf("渠道 %d 状态 = %q, 期望 pending(待补齐)", chID, state)
		}
	}
}

// TestProcessFallsBackWhenPrimaryFails 验证上传降级:主渠道写入失败时
// 自动顺延下一健康渠道,不整体报错。
func TestProcessFallsBackWhenPrimaryFails(t *testing.T) {
	testenv.SkipIfRoot(t)
	env := newTestEnv(t, nil)
	brokenDir := t.TempDir()
	brokenID, err := env.Manager.Create(t.Context(), channel.CreateParams{
		Name: "broken-primary", Driver: "local", Config: map[string]string{"path": brokenDir}, Priority: 300,
	})
	if err != nil {
		t.Fatalf("创建渠道: %v", err)
	}
	// 主渠道目录不可写,写入必失败。
	if err := os.Chmod(brokenDir, 0o000); err != nil {
		t.Fatalf("模拟主渠道故障: %v", err)
	}
	defer func() {
		if cerr := os.Chmod(brokenDir, 0o700); cerr != nil {
			t.Errorf("恢复目录权限: %v", cerr)
		}
	}()

	res, err := env.svc.Process(t.Context(), pngBytes(t), upload.Options{
		UserID:   sqlc.DefaultUserID,
		FileName: "a.png", EXIFPolicy: imaging.PolicyPassthrough,
	})
	if err != nil {
		t.Fatalf("主渠道故障时上传应降级成功, 实际错误: %v", err)
	}
	row, err := env.Queries.GetImageBlob(t.Context(), liveImage(res.ImageID))
	if err != nil {
		t.Fatalf("查询引用记录: %v", err)
	}
	replicas, err := env.Queries.ListBlobChannels(t.Context(), row.BlobID)
	if err != nil {
		t.Fatalf("查询副本状态: %v", err)
	}
	for _, rep := range replicas {
		if rep.ChannelID == brokenID && rep.State == channel.StateDone {
			t.Fatal("故障主渠道不应有就绪副本")
		}
		if rep.ChannelID != brokenID && rep.State != channel.StateDone {
			t.Errorf("降级渠道 %d 状态 = %q, 期望 done", rep.ChannelID, rep.State)
		}
	}
}

// TestProcessTruncatesLongFileName 验证超长文件名被 UTF-8 安全截断到 255 字节。
func TestProcessTruncatesLongFileName(t *testing.T) {
	env := newTestEnv(t, nil)
	longName := strings.Repeat("图", 100) + ".png" // 304 字节,截断点落在多字节字符上

	res, err := env.svc.Process(t.Context(), pngBytes(t), upload.Options{
		UserID:     sqlc.DefaultUserID,
		FileName:   longName,
		EXIFPolicy: imaging.PolicyPassthrough,
	})
	if err != nil {
		t.Fatalf("Process 返回错误: %v", err)
	}
	row, err := env.Queries.GetImageBlob(t.Context(), liveImage(res.ImageID))
	if err != nil {
		t.Fatalf("查询引用记录: %v", err)
	}
	if len(row.FileName) > 255 {
		t.Fatalf("文件名长度 = %d 字节, 期望 ≤ 255", len(row.FileName))
	}
	if !strings.HasPrefix(longName, row.FileName) || !utf8.ValidString(row.FileName) {
		t.Fatalf("截断结果不是合法 UTF-8 前缀: %q", row.FileName)
	}
}

// TestProcessRejectsUnsupportedType 验证非白名单类型返回 ErrUnsupportedType。
func TestProcessRejectsUnsupportedType(t *testing.T) {
	env := newTestEnv(t, nil)
	_, err := env.svc.Process(t.Context(), []byte("just some text"), upload.Options{
		UserID: sqlc.DefaultUserID, FileName: "a.txt", EXIFPolicy: imaging.PolicyPassthrough,
	})
	if !errors.Is(err, upload.ErrUnsupportedType) {
		t.Fatalf("错误 = %v, 期望 ErrUnsupportedType", err)
	}
}

// TestProcessRejectsTooManyPixels 验证超出像素预算返回 ErrTooManyPixels(不进解码器)。
func TestProcessRejectsTooManyPixels(t *testing.T) {
	env := newTestEnv(t, func(s *config.Settings) { s.MaxPixelsMP = 1 }) // 预算 1 百万像素,1024x1024 必超
	_, err := env.svc.Process(t.Context(), bigPNG(t), upload.Options{
		UserID: sqlc.DefaultUserID, FileName: "a.png", EXIFPolicy: imaging.PolicyPassthrough,
	})
	if !errors.Is(err, upload.ErrTooManyPixels) {
		t.Fatalf("错误 = %v, 期望 ErrTooManyPixels", err)
	}
}

// TestProcessIdempotency 验证同一幂等键重复上传返回同一 ImageID。
func TestProcessIdempotency(t *testing.T) {
	env := newTestEnv(t, nil)
	payload := pngBytes(t)

	first, err := env.svc.Process(t.Context(), payload, upload.Options{
		UserID: sqlc.DefaultUserID, FileName: "a.png", IdempotencyKey: "retry-key-1",
		EXIFPolicy: imaging.PolicyPassthrough,
	})
	if err != nil {
		t.Fatalf("第一次 Process: %v", err)
	}
	second, err := env.svc.Process(t.Context(), payload, upload.Options{
		UserID: sqlc.DefaultUserID, FileName: "a.png", IdempotencyKey: "retry-key-1",
		EXIFPolicy: imaging.PolicyPassthrough,
	})
	if err != nil {
		t.Fatalf("第二次 Process: %v", err)
	}
	if first.ImageID != second.ImageID {
		t.Fatalf("幂等键失效: %q ≠ %q", first.ImageID, second.ImageID)
	}
}

// TestProcessIdempotencyIsolatedPerUser 验证幂等键按用户隔离:两个用户用同一个键各得一张图,
// 各自重试仍只命中自己的那张。
func TestProcessIdempotencyIsolatedPerUser(t *testing.T) {
	env := newTestEnv(t, nil)
	const otherUser = "u-other"
	if err := env.Queries.CreateUser(t.Context(), sqlc.CreateUserParams{
		ID: otherUser, Name: "other", Role: "user",
	}); err != nil {
		t.Fatalf("建第二个用户: %v", err)
	}
	uploadAs := func(userID string) string {
		t.Helper()
		res, err := env.svc.Process(t.Context(), pngBytes(t), upload.Options{
			UserID: userID, FileName: "a.png", IdempotencyKey: "shared-key", EXIFPolicy: imaging.PolicyPassthrough,
		})
		if err != nil {
			t.Fatalf("用户 %s 上传: %v", userID, err)
		}
		return res.ImageID
	}
	mine, theirs := uploadAs(sqlc.DefaultUserID), uploadAs(otherUser)
	if mine == theirs {
		t.Fatalf("不同用户的同名幂等键命中了同一张图: %q", mine)
	}
	if again := uploadAs(sqlc.DefaultUserID); again != mine {
		t.Fatalf("默认用户重试得 %q, 期望仍是自己的 %q", again, mine)
	}
	if again := uploadAs(otherUser); again != theirs {
		t.Fatalf("第二个用户重试得 %q, 期望仍是自己的 %q", again, theirs)
	}
}

// TestProcessMinSuccessWritesReplicasSynchronously 验证 min_success=2 时两个健康渠道都同步写入:
// 两行副本状态均为 done,两个渠道目录都已有对象。
func TestProcessMinSuccessWritesReplicasSynchronously(t *testing.T) {
	env := newTestEnv(t, func(s *config.Settings) { s.MinSuccess = 2 })
	secondID, secondDir := env.AddLocalChannel(t, "second", 200, false)

	row, replicas := env.uploadPNG(t)
	if len(replicas) != 2 {
		t.Fatalf("副本记录数 = %d, 期望 2", len(replicas))
	}
	for _, rep := range replicas {
		if rep.State != channel.StateDone {
			t.Errorf("渠道 %d 状态 = %q, 期望 done(两份都同步写入)", rep.ChannelID, rep.State)
		}
	}
	for _, dir := range []string{secondDir, filepath.Join(env.DataDir, "blobs")} {
		if _, err := os.Stat(filepath.Join(dir, row.StorageKey)); err != nil {
			t.Errorf("渠道目录 %s 缺少对象: %v", dir, err)
		}
	}
	if _, ok := env.Manager.Get(secondID); !ok {
		t.Fatal("第二个渠道句柄丢失")
	}
}

// TestProcessMinSuccessUnmetRejects 验证健康渠道数凑不够 min_success 时上传明确失败,
// 不落任何引用或物理图记录。
func TestProcessMinSuccessUnmetRejects(t *testing.T) {
	env := newTestEnv(t, func(s *config.Settings) { s.MinSuccess = 2 })

	_, err := env.svc.Process(t.Context(), pngBytes(t), upload.Options{
		UserID: sqlc.DefaultUserID, FileName: "a.png", EXIFPolicy: imaging.PolicyPassthrough,
	})
	if !errors.Is(err, upload.ErrInsufficientReplicas) {
		t.Fatalf("错误 = %v, 期望 ErrInsufficientReplicas", err)
	}
	images, err := env.Queries.CountImages(t.Context())
	if err != nil {
		t.Fatalf("统计引用: %v", err)
	}
	if images != 0 {
		t.Fatalf("副本数不足仍落了 %d 条引用", images)
	}
	var blobs int
	if err := env.Conn.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM blobs").Scan(&blobs); err != nil {
		t.Fatalf("统计物理图: %v", err)
	}
	if blobs != 0 {
		t.Fatalf("副本数不足仍落了 %d 条物理图", blobs)
	}
}

// liveImage 构造按当前时刻查询未到期引用的参数。
func liveImage(id string) sqlc.GetImageBlobParams {
	return sqlc.GetImageBlobParams{ID: id, Now: sqlc.TimeText(time.Now())}
}

// uploadPNG 以默认用户、原样保留元数据上传一张小 PNG,读回其引用记录与各渠道的副本状态。
// @return row 新图的引用与物理图记录
// @return replicas 物理图在各渠道的副本状态
func (e *testEnv) uploadPNG(t *testing.T) (sqlc.GetImageBlobRow, []sqlc.ListBlobChannelsRow) {
	t.Helper()
	res, err := e.svc.Process(t.Context(), pngBytes(t), upload.Options{
		UserID: sqlc.DefaultUserID, FileName: "a.png", EXIFPolicy: imaging.PolicyPassthrough,
	})
	if err != nil {
		t.Fatalf("Process 返回错误: %v", err)
	}
	row, err := e.Queries.GetImageBlob(t.Context(), liveImage(res.ImageID))
	if err != nil {
		t.Fatalf("查询引用记录: %v", err)
	}
	replicas, err := e.Queries.ListBlobChannels(t.Context(), row.BlobID)
	if err != nil {
		t.Fatalf("查询副本状态: %v", err)
	}
	return row, replicas
}
