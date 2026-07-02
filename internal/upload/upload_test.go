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
	"path/filepath"
	"strings"
	"testing"

	"github.com/x-haose/imgyoung/internal/db"
	"github.com/x-haose/imgyoung/internal/storage"
	"github.com/x-haose/imgyoung/internal/upload"
)

// testEnv 上传服务测试环境:临时 DB + 临时 local 渠道。
type testEnv struct {
	svc     *upload.Service
	queries *db.Queries
	driver  storage.Driver
}

// newTestEnv 组装测试环境,maxPixels 为像素预算上限。
func newTestEnv(t *testing.T, maxPixels int64) *testEnv {
	t.Helper()
	dir := t.TempDir()
	conn, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("打开测试数据库: %v", err)
	}
	t.Cleanup(func() {
		if cerr := conn.Close(); cerr != nil {
			t.Errorf("关闭测试数据库: %v", cerr)
		}
	})
	if err := db.Migrate(conn); err != nil {
		t.Fatalf("迁移测试数据库: %v", err)
	}
	driver, err := storage.NewRcloneDriver(t.Context(), filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatalf("创建测试存储驱动: %v", err)
	}
	queries := db.New(conn)
	svc := upload.New(upload.Config{
		Conn:      conn,
		Queries:   queries,
		Driver:    driver,
		MaxPixels: maxPixels,
	})
	return &testEnv{svc: svc, queries: queries, driver: driver}
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
	seg = binary.BigEndian.AppendUint16(seg, uint16(len(payload)+2)) //nolint:gosec // 测试载荷为几十字节常量,远小于 uint16 上限
	seg = append(seg, payload...)

	out := append([]byte{}, jpg[:2]...)
	out = append(out, seg...)
	out = append(out, jpg[2:]...)
	return out
}

// fetchStored 按 imageID 取出渠道内的实际存储字节。
func fetchStored(t *testing.T, env *testEnv, imageID string) []byte {
	t.Helper()
	row, err := env.queries.GetImageBlob(t.Context(), imageID)
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
	env := newTestEnv(t, 1<<20)
	payload := pngBytes(t)

	res, err := env.svc.Process(t.Context(), payload, upload.Options{FileName: "a.png", EXIFPolicy: upload.PolicyPassthrough})
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

	row, err := env.queries.GetImageBlob(t.Context(), res.ImageID)
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
// 且哈希基于净化后的字节(设计定稿:先净化后哈希)。
func TestProcessStripsJPEGMetadata(t *testing.T) {
	env := newTestEnv(t, 1<<20)
	payload := jpegWithEXIF(t)

	res, err := env.svc.Process(t.Context(), payload, upload.Options{
		FileName:   "a.jpg",
		EXIFPolicy: upload.PolicyStrip,
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
	row, err := env.queries.GetImageBlob(t.Context(), res.ImageID)
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
	env := newTestEnv(t, 1<<20)
	payload := jpegWithEXIF(t)

	res, err := env.svc.Process(t.Context(), payload, upload.Options{
		FileName:   "a.jpg",
		EXIFPolicy: upload.PolicyPassthrough,
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
	env := newTestEnv(t, 1<<20)
	for _, policy := range []upload.EXIFPolicy{"", "spoof", "bogus"} {
		_, err := env.svc.Process(t.Context(), pngBytes(t), upload.Options{
			FileName:   "a.png",
			EXIFPolicy: policy,
		})
		if !errors.Is(err, upload.ErrUnknownPolicy) {
			t.Fatalf("policy=%q 错误 = %v, 期望 ErrUnknownPolicy", policy, err)
		}
	}
}

// TestProcessRejectsUnsupportedType 验证非白名单类型返回 ErrUnsupportedType。
func TestProcessRejectsUnsupportedType(t *testing.T) {
	env := newTestEnv(t, 1<<20)
	_, err := env.svc.Process(t.Context(), []byte("just some text"), upload.Options{FileName: "a.txt", EXIFPolicy: upload.PolicyPassthrough})
	if !errors.Is(err, upload.ErrUnsupportedType) {
		t.Fatalf("错误 = %v, 期望 ErrUnsupportedType", err)
	}
}

// TestProcessRejectsTooManyPixels 验证超出像素预算返回 ErrTooManyPixels(不进解码器)。
func TestProcessRejectsTooManyPixels(t *testing.T) {
	env := newTestEnv(t, 1) // 预算 1 像素,2x2 必超
	_, err := env.svc.Process(t.Context(), pngBytes(t), upload.Options{FileName: "a.png", EXIFPolicy: upload.PolicyPassthrough})
	if !errors.Is(err, upload.ErrTooManyPixels) {
		t.Fatalf("错误 = %v, 期望 ErrTooManyPixels", err)
	}
}

// TestProcessIdempotency 验证同一幂等键重复上传返回同一 ImageID。
func TestProcessIdempotency(t *testing.T) {
	env := newTestEnv(t, 1<<20)
	payload := pngBytes(t)

	first, err := env.svc.Process(t.Context(), payload, upload.Options{FileName: "a.png", IdempotencyKey: "retry-key-1", EXIFPolicy: upload.PolicyPassthrough})
	if err != nil {
		t.Fatalf("第一次 Process: %v", err)
	}
	second, err := env.svc.Process(t.Context(), payload, upload.Options{FileName: "a.png", IdempotencyKey: "retry-key-1", EXIFPolicy: upload.PolicyPassthrough})
	if err != nil {
		t.Fatalf("第二次 Process: %v", err)
	}
	if first.ImageID != second.ImageID {
		t.Fatalf("幂等键失效: %q ≠ %q", first.ImageID, second.ImageID)
	}
}
