package rclone

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/cache"
	"github.com/rclone/rclone/fs/config/obscure"
	"github.com/rclone/rclone/fs/fspath"

	"github.com/x-haose/imgyoung/store/driver"
)

// openLocal 在临时目录上打开一个 local 后端驱动,测试结束自动关闭。
// @param dir 后端根目录
// @return 就绪的驱动
func openLocal(t *testing.T, dir string) *Driver {
	t.Helper()
	d, err := Open(t.Context(), Options{Backend: "local", Path: dir})
	if err != nil {
		t.Fatalf("Open 返回错误: %v", err)
	}
	t.Cleanup(func() {
		if cerr := d.Close(t.Context()); cerr != nil {
			t.Errorf("Close 返回错误: %v", cerr)
		}
	})
	return d
}

// TestRcloneDriverRoundTrip 验证 Put → Stat → Open → Delete 全链路行为正确。
// @Description: 使用 rclone local backend + 临时目录,零外部依赖。
func TestRcloneDriverRoundTrip(t *testing.T) {
	ctx := t.Context()
	d := openLocal(t, t.TempDir())

	const key = "ab/test.bin"
	payload := []byte("hello imgyoung")

	if err := d.Put(ctx, key, bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatalf("Put 返回错误: %v", err)
	}

	info, err := d.Stat(ctx, key)
	if err != nil {
		t.Fatalf("Stat 返回错误: %v", err)
	}
	if info.Size != int64(len(payload)) {
		t.Fatalf("Stat.Size = %d, 期望 %d", info.Size, len(payload))
	}

	rc, err := d.Open(ctx, key)
	if err != nil {
		t.Fatalf("Open 返回错误: %v", err)
	}
	got, err := io.ReadAll(rc)
	if cerr := rc.Close(); cerr != nil {
		t.Fatalf("Close 返回错误: %v", cerr)
	}
	if err != nil {
		t.Fatalf("读取对象内容返回错误: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("读回内容 = %q, 期望 %q", got, payload)
	}

	if err := d.Delete(ctx, key); err != nil {
		t.Fatalf("Delete 返回错误: %v", err)
	}
	if _, err := d.Stat(ctx, key); !errors.Is(err, driver.ErrNotFound) {
		t.Fatalf("删除后 Stat 错误 = %v, 期望 driver.ErrNotFound", err)
	}
}

// TestRcloneDriverList 验证递归列举:返回全部对象的键/大小/修改时间,空目录返回空。
func TestRcloneDriverList(t *testing.T) {
	ctx := t.Context()
	d := openLocal(t, t.TempDir())

	empty, err := d.List(ctx, "")
	if err != nil {
		t.Fatalf("列举空渠道: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("空渠道列举出 %d 个对象", len(empty))
	}
	missing, err := d.List(ctx, "no/such/dir")
	if err != nil || len(missing) != 0 {
		t.Fatalf("列举不存在的目录 = %v, %v, 期望空且无错误", missing, err)
	}

	before := time.Now().Add(-time.Minute)
	for _, key := range []string{"a.bin", "sub/b.bin", "_imgyoung/backup/c.db"} {
		payload := []byte("x-" + key)
		if err := d.Put(ctx, key, bytes.NewReader(payload), int64(len(payload))); err != nil {
			t.Fatalf("写入 %s: %v", key, err)
		}
	}

	all, err := d.List(ctx, "")
	if err != nil {
		t.Fatalf("List 返回错误: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("对象数 = %d, 期望 3(递归含子目录)", len(all))
	}
	seen := map[string]driver.ObjectInfo{}
	for _, o := range all {
		seen[o.Key] = o
	}
	if o, ok := seen["sub/b.bin"]; !ok || o.Size != int64(len("x-sub/b.bin")) {
		t.Fatalf("子目录对象缺失或大小错误: %+v", seen)
	}
	if seen["a.bin"].ModTime.Before(before) {
		t.Fatalf("ModTime 未填充: %v", seen["a.bin"].ModTime)
	}

	// 按目录前缀列举:只返回该目录下的对象。
	backups, err := d.List(ctx, "_imgyoung/backup")
	if err != nil {
		t.Fatalf("按前缀列举: %v", err)
	}
	if len(backups) != 1 || backups[0].Key != "_imgyoung/backup/c.db" {
		t.Fatalf("前缀列举结果 = %+v, 期望仅 c.db 且键为完整路径", backups)
	}
}

// TestRcloneDriverListShallow 验证只列一层:渠道根只返回根下的对象,子目录只返回该目录这一层,
// 不存在的目录返回空且无错误。
func TestRcloneDriverListShallow(t *testing.T) {
	ctx := t.Context()
	d := openLocal(t, t.TempDir())
	for _, key := range []string{"a.bin", "sub/b.bin", "sub/deep/c.bin"} {
		payload := []byte("x-" + key)
		if err := d.Put(ctx, key, bytes.NewReader(payload), int64(len(payload))); err != nil {
			t.Fatalf("写入 %s: %v", key, err)
		}
	}
	cases := map[string][]string{"": {"a.bin"}, "sub": {"sub/b.bin"}, "no/such/dir": nil}
	for dir, want := range cases {
		objs, err := d.ListShallow(ctx, dir)
		if err != nil {
			t.Fatalf("ListShallow(%q) 返回错误: %v", dir, err)
		}
		got := make([]string, 0, len(objs))
		for _, o := range objs {
			got = append(got, o.Key)
		}
		if !slices.Equal(got, want) {
			t.Errorf("ListShallow(%q) = %v, 期望 %v", dir, got, want)
		}
	}
}

// TestRcloneDriverListFileAsDir 验证列举的路径上是对象而不是目录时,两种列举都视为空目录、不报错。
func TestRcloneDriverListFileAsDir(t *testing.T) {
	ctx := t.Context()
	d := openLocal(t, t.TempDir())
	if err := d.Put(ctx, "_imgyoung", bytes.NewReader([]byte("x")), 1); err != nil {
		t.Fatalf("写入同名对象: %v", err)
	}
	if objs, err := d.List(ctx, "_imgyoung"); err != nil || len(objs) != 0 {
		t.Fatalf("List = %v, %v, 期望空且无错误", objs, err)
	}
	if objs, err := d.ListShallow(ctx, "_imgyoung"); err != nil || len(objs) != 0 {
		t.Fatalf("ListShallow = %v, %v, 期望空且无错误", objs, err)
	}
}

// TestRcloneDriverOpenMissing 验证读取不存在的对象返回 driver.ErrNotFound 哨兵错误。
func TestRcloneDriverOpenMissing(t *testing.T) {
	ctx := t.Context()
	d := openLocal(t, t.TempDir())
	if _, err := d.Open(ctx, "no/such/object"); !errors.Is(err, driver.ErrNotFound) {
		t.Fatalf("Open 缺失对象错误 = %v, 期望 driver.ErrNotFound", err)
	}
}

// TestOpenRejectsUnknownBackend 验证未知后端名明确报错。
func TestOpenRejectsUnknownBackend(t *testing.T) {
	if _, err := Open(t.Context(), Options{Backend: "没有这个后端"}); err == nil {
		t.Fatal("未知后端应报错")
	}
}

// TestOpenCryptRoundTrip 验证加密层:落盘为密文与加密文件名,经驱动读回为明文;
// Close 后内层实例从 rclone fs 缓存移除。
func TestOpenCryptRoundTrip(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	before := cache.Entries()
	d, err := Open(ctx, Options{
		Backend: "local", Path: dir,
		Crypt: &CryptOptions{Password: "content-pass", Salt: "name-salt"},
	})
	if err != nil {
		t.Fatalf("Open 返回错误: %v", err)
	}
	if cache.Entries() != before+1 {
		t.Fatalf("fs 缓存项数 = %d, 期望内层登记后为 %d", cache.Entries(), before+1)
	}

	plain := []byte("plaintext-image-bytes-0123456789")
	if err := d.Put(ctx, "img.bin", bytes.NewReader(plain), int64(len(plain))); err != nil {
		t.Fatalf("写入加密驱动: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("加密目录为空或不可读: %v", err)
	}
	for _, e := range entries {
		if e.Name() == "img.bin" {
			t.Fatal("文件名未被加密")
		}
		raw, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			t.Fatalf("读密文文件: %v", rerr)
		}
		if bytes.Contains(raw, plain) {
			t.Fatal("磁盘文件含明文内容")
		}
	}
	rc, err := d.Open(ctx, "img.bin")
	if err != nil {
		t.Fatalf("读回对象: %v", err)
	}
	got, err := io.ReadAll(rc)
	if cerr := rc.Close(); cerr != nil {
		t.Fatalf("关闭对象流: %v", cerr)
	}
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("读回明文不一致: err=%v", err)
	}

	if err := d.Close(ctx); err != nil {
		t.Fatalf("Close 返回错误: %v", err)
	}
	if cache.Entries() != before {
		t.Fatalf("Close 后 fs 缓存项数 = %d, 期望回到 %d", cache.Entries(), before)
	}
}

// TestParamStoreWriteBack 验证后端写回经 Set 更新映射并触发持久化回调;空值视为未设置。
func TestParamStoreWriteBack(t *testing.T) {
	var gotKey, gotValue string
	store := &paramStore{
		values:   map[string]string{"token": "old", "empty": ""},
		onChange: func(key, value string) { gotKey, gotValue = key, value },
	}
	if v, ok := store.Get("token"); !ok || v != "old" {
		t.Fatalf("Get(token) = %q/%v, 期望 old/true", v, ok)
	}
	if _, ok := store.Get("empty"); ok {
		t.Fatal("空值应视为未设置,交给后端默认值")
	}
	store.Set("token", "new")
	if gotKey != "token" || gotValue != "new" {
		t.Fatalf("持久化回调收到 %q=%q, 期望 token=new", gotKey, gotValue)
	}
	if v, _ := store.Get("token"); v != "new" {
		t.Fatalf("写回后 Get(token) = %q, 期望 new", v)
	}
}

// TestWithDefaults 验证 s3 默认跳过桶检查,且不覆盖显式值、不波及其他后端、不修改入参。
func TestWithDefaults(t *testing.T) {
	in := map[string]string{"access_key_id": "AK"}
	out := withDefaults("s3", in)
	if out["no_check_bucket"] != "true" {
		t.Errorf("s3 默认 no_check_bucket = %q, 期望 true", out["no_check_bucket"])
	}
	if _, ok := in["no_check_bucket"]; ok {
		t.Error("入参被原地修改")
	}
	if got := withDefaults("s3", map[string]string{"no_check_bucket": "false"}); got["no_check_bucket"] != "false" {
		t.Errorf("显式值被覆盖 = %q, 期望保留 false", got["no_check_bucket"])
	}
	if got := withDefaults("local", nil); len(got) != 0 {
		t.Errorf("非 s3 后端被注入参数: %v", got)
	}
}

// TestCryptParamsRevealable 验证 crypt 层参数:remote 原样指向内层、密码经编码可还原。
func TestCryptParamsRevealable(t *testing.T) {
	params, err := cryptParams("imgyoung-test:bucket/dir", &CryptOptions{Password: "file-pass", Salt: "salt-pass"})
	if err != nil {
		t.Fatalf("cryptParams 返回错误: %v", err)
	}
	if params["remote"] != "imgyoung-test:bucket/dir" {
		t.Errorf("remote = %q, 期望 imgyoung-test:bucket/dir", params["remote"])
	}
	for key, want := range map[string]string{"password": "file-pass", "password2": "salt-pass"} {
		if got, err := obscure.Reveal(params[key]); err != nil || got != want {
			t.Errorf("%s 还原 = %q(err=%v), 期望 %q", key, got, err, want)
		}
	}
	if params["filename_encryption"] != "standard" {
		t.Errorf("filename_encryption = %q, 期望 standard", params["filename_encryption"])
	}
}

// TestNewNameIsValidConfigName 验证生成的远端名符合 rclone 对配置名的字符要求。
func TestNewNameIsValidConfigName(t *testing.T) {
	a, b := newName(), newName()
	if err := fspath.CheckConfigName(a); err != nil {
		t.Fatalf("远端名 %q 不合法: %v", a, err)
	}
	if a == b {
		t.Fatalf("两次生成的远端名相同: %q", a)
	}
}

// TestConfigFileUnrepresentableHidesValue 参数值含换行或以三个双引号开头时导出被拒,错误指出远端与参数键,不带参数值的任何片段。
func TestConfigFileUnrepresentableHidesValue(t *testing.T) {
	const tail = "secret-after-newline"
	for name, value := range map[string]string{
		"含换行":     "https://x\n" + tail,
		"三个双引号开头": `"""https://x` + tail,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ConfigFile(Options{Backend: "local", Path: "/data", Params: map[string]string{
				"links": value,
			}}, "imgyoung-1", "imgyoung-1-crypt")
			ue, ok := errors.AsType[*UnrepresentableError](err)
			if !ok || !errors.Is(err, ErrUnrepresentable) {
				t.Fatalf("期望 *UnrepresentableError,得到 %v", err)
			}
			if ue.Remote != "imgyoung-1" || ue.Key != "links" {
				t.Fatalf("错误指出的位置 = %s/%s,期望 imgyoung-1/links", ue.Remote, ue.Key)
			}
			if strings.Contains(err.Error(), tail) || strings.Contains(err.Error(), "https://x") {
				t.Fatalf("错误里带出了参数值: %v", err)
			}
		})
	}
}

// TestObscureSecrets 验证密码类参数的编码:明文编码、非密码参数不动、已编码值在宽松模式下保留。
func TestObscureSecrets(t *testing.T) {
	const plain = "明文密码"
	params := map[string]string{"user": "u@example.com", "pass": plain, "url": "https://dav.example.com/"}
	if err := ObscureSecrets("webdav", params); err != nil {
		t.Fatalf("ObscureSecrets 返回错误: %v", err)
	}
	if params["user"] != "u@example.com" || params["url"] != "https://dav.example.com/" {
		t.Errorf("非密码参数被改动: %v", params)
	}
	encoded := params["pass"]
	if got, err := obscure.Reveal(encoded); err != nil || got != plain {
		t.Fatalf("编码结果无法还原: %q(err=%v)", got, err)
	}

	// 宽松模式:已编码的值原样保留,明文照常编码。
	again := map[string]string{"pass": encoded}
	if err := ObscurePlainSecrets("webdav", again); err != nil {
		t.Fatalf("ObscurePlainSecrets 返回错误: %v", err)
	}
	if again["pass"] != encoded {
		t.Error("已编码的值被二次编码")
	}
	raw := map[string]string{"pass": "short"}
	if err := ObscurePlainSecrets("webdav", raw); err != nil {
		t.Fatalf("ObscurePlainSecrets 返回错误: %v", err)
	}
	if got, err := obscure.Reveal(raw["pass"]); err != nil || got != "short" {
		t.Errorf("明文未被编码: %q(err=%v)", got, err)
	}

	if err := ObscureSecrets("没有这个后端", map[string]string{}); err == nil {
		t.Error("未知后端应报错")
	}
}

// TestPutSetsContentType 验证写入时按内容嗅探出的 Content-Type 交给后端(内存后端会保存它),
// 且嗅探读过的开头字节不丢。
func TestPutSetsContentType(t *testing.T) {
	ctx := t.Context()
	d, err := Open(ctx, Options{Backend: "memory", Path: "content-type"})
	if err != nil {
		t.Fatalf("打开内存驱动: %v", err)
	}
	t.Cleanup(func() { _ = d.Close(context.Background()) })

	payload := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 1024)...)
	if err := d.Put(ctx, "no-extension", bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatalf("写入: %v", err)
	}
	obj, err := d.f.NewObject(ctx, "no-extension")
	if err != nil {
		t.Fatalf("查找对象: %v", err)
	}
	typer, ok := obj.(fs.MimeTyper)
	if !ok || typer.MimeType(ctx) != "image/png" {
		t.Fatalf("对象类型未按内容设置(ok=%v)", ok)
	}
	rc, err := d.Open(ctx, "no-extension")
	if err != nil {
		t.Fatalf("读回: %v", err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("读回字节不一致(err=%v)", err)
	}
}

// TestConstructContextDetaches 验证构造用的上下文在构造期间随调用方取消,脱钩之后不再随之取消——
// 后端留存它发后续请求,调用方的请求结束不能连带作废驱动。
func TestConstructContextDetaches(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	bctx, detach := constructContext(ctx)
	if !detach() {
		t.Fatal("ctx 尚未取消,脱钩却报告已结束")
	}
	cancel()
	if bctx.Err() != nil {
		t.Fatalf("脱钩后调用方取消仍波及构造上下文: %v", bctx.Err())
	}

	ctx, cancel = context.WithCancel(t.Context())
	bctx, detach = constructContext(ctx)
	cancel()
	<-bctx.Done()
	if detach() {
		t.Fatal("ctx 已在构造期间取消,脱钩却报告成功")
	}
}
