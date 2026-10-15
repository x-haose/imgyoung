package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db/dbtest"
	"github.com/x-haose/imgyoung/internal/infra/secret"
)

// newKey 生成一把主密钥。
func newKey(t *testing.T) *secret.MasterKey {
	t.Helper()
	k, err := secret.NewMasterKey()
	if err != nil {
		t.Fatalf("生成主密钥: %v", err)
	}
	return k
}

// TestLoadMasterKeyWithoutFingerprint 验证没有指纹的旧库:只有派生数据(加密渠道)时不生成新密钥、缺钥拒绝启动;
// 混有两把密钥的密文时,能解开任意一份的密钥被接受并记下指纹,两份都解不开的被拒。
func TestLoadMasterKeyWithoutFingerprint(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)

	t.Run("只有派生数据时缺钥拒绝", func(t *testing.T) {
		conn, queries := dbtest.New(t)
		if _, err := conn.ExecContext(t.Context(),
			`INSERT INTO channels (name, driver, encrypted) VALUES ('enc', 'local', 1)`); err != nil {
			t.Fatalf("写入加密渠道: %v", err)
		}
		dir := t.TempDir()
		if _, err := loadMasterKey(t.Context(), config.Bootstrap{DataDir: dir}, queries, logger); err == nil {
			t.Fatal("库里已有依赖主密钥的数据,缺钥时应拒绝启动")
		}
		if _, err := os.Stat(filepath.Join(dir, secret.KeyFileName)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("缺钥时不应生成新密钥文件: %v", err)
		}
	})

	t.Run("混有两把密钥的密文", func(t *testing.T) {
		older, current := newKey(t), newKey(t)
		for _, tc := range []struct {
			key     *secret.MasterKey
			wantErr bool
		}{{current, false}, {older, false}, {newKey(t), true}} {
			conn, queries := dbtest.New(t)
			for i, k := range []*secret.MasterKey{older, current} {
				sealed, err := k.EncryptString(`{"path":"/data"}`)
				if err != nil {
					t.Fatalf("加密: %v", err)
				}
				if _, err := conn.ExecContext(t.Context(),
					`INSERT INTO channels (name, driver, config_sealed) VALUES (?, 'local', ?)`,
					fmt.Sprintf("c%d", i), sealed); err != nil {
					t.Fatalf("写入渠道: %v", err)
				}
			}
			boot := config.Bootstrap{DataDir: t.TempDir(), MasterKey: tc.key.Hex()}
			_, err := loadMasterKey(t.Context(), boot, queries, logger)
			if tc.wantErr != errors.Is(err, errMasterKeyMismatch) || (!tc.wantErr && err != nil) {
				t.Fatalf("期望拒绝=%v, 得 %v", tc.wantErr, err)
			}
		}
	})
}
