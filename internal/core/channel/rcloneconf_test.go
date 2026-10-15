package channel_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/cache"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/testenv"
)

// TestRcloneConfigRoundTrip 验证导出的 rclone 配置与本服务驱动等价:驱动写入的对象,只凭配置文本经标准 rclone 读回一致。
// @Description: 加密与不加密渠道各一,文件头分别用中文与英文注释。配置文本经 rclone 自己的配置文件加载路径
// (configfile + fs.NewFs)解析,与用户执行 rclone --config 相同;加密渠道经 crypt 远端读回明文,而直接看后端远端
// 只有密文、找不到原对象名。
func TestRcloneConfigRoundTrip(t *testing.T) {
	env := testenv.New(t)
	langs := map[bool]channel.Language{false: channel.LanguageEnglish, true: channel.LanguageChinese}
	for _, encrypted := range []bool{false, true} {
		t.Run(fmt.Sprintf("encrypted=%v", encrypted), func(t *testing.T) {
			id, dir := env.AddLocalChannel(t, fmt.Sprintf("conf-%v", encrypted), 50, encrypted)
			h, ok := env.Manager.Get(id)
			if !ok {
				t.Fatalf("渠道 %d 未装载", id)
			}
			const key = "ab/object.bin"
			payload := []byte("rclone-config-round-trip-payload")
			if err := h.Driver.Put(t.Context(), key, bytes.NewReader(payload), int64(len(payload))); err != nil {
				t.Fatalf("经驱动写入: %v", err)
			}

			file, err := env.Manager.RcloneConfig(t.Context(), id, langs[encrypted])
			if err != nil {
				t.Fatalf("生成 rclone 配置: %v", err)
			}
			text := file.Content
			confPath := filepath.Join(t.TempDir(), file.Name)
			if err := os.WriteFile(confPath, []byte(text), 0o600); err != nil {
				t.Fatalf("写配置文件: %v", err)
			}
			testenv.UseRcloneConfig(t, confPath)

			backend := fmt.Sprintf("imgyoung-%d", id)
			t.Cleanup(func() { cache.ClearConfig(backend) })
			root := backend + ":" + dir
			if encrypted {
				root = backend + "-crypt:"
				if _, err := readObject(t, backend+":"+dir, key); !errors.Is(err, fs.ErrorObjectNotFound) {
					t.Fatalf("后端远端直接读到了原对象名(err=%v),期望只有加密后的对象名", err)
				}
			}
			got, err := readObject(t, root, key)
			if err != nil {
				t.Fatalf("凭配置文件经 %s 读回: %v\n配置:\n%s", root, err, text)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("读回内容 = %q, 期望 %q", got, payload)
			}
		})
	}
}

// readObject 按 rclone 路径构造 Fs 并读出一个对象的全部内容。
// @param root rclone 路径,形如 remote:dir
// @param key 对象键
// @return 对象内容
// @return 构造、查找或读取失败时的错误
func readObject(t *testing.T, root, key string) ([]byte, error) {
	t.Helper()
	f, err := fs.NewFs(t.Context(), root)
	if err != nil {
		return nil, fmt.Errorf("构造 %s: %w", root, err)
	}
	obj, err := f.NewObject(t.Context(), key)
	if err != nil {
		return nil, err
	}
	rc, err := obj.Open(t.Context())
	if err != nil {
		return nil, fmt.Errorf("打开对象: %w", err)
	}
	data, err := io.ReadAll(rc)
	if cerr := rc.Close(); cerr != nil && err == nil {
		err = cerr
	}
	return data, err
}
