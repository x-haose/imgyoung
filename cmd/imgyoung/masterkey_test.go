package main

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db/dbtest"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/secret"
)

// TestLoadMasterKeyFingerprint 验证全新库生成主密钥并记下指纹;库里没有指纹却已有任一类依赖主密钥的数据时拒绝启动。
func TestLoadMasterKeyFingerprint(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)

	t.Run("全新库生成主密钥并记下指纹", func(t *testing.T) {
		_, queries := dbtest.New(t)
		key, err := loadMasterKey(t.Context(), config.Bootstrap{DataDir: t.TempDir()}, queries, logger)
		if err != nil {
			t.Fatalf("全新库应正常启动: %v", err)
		}
		stored, err := queries.GetMasterKeyFingerprint(t.Context())
		if err != nil {
			t.Fatalf("读取指纹: %v", err)
		}
		if ok, err := key.Matches(stored); err != nil || !ok {
			t.Fatalf("记下的指纹与主密钥不符: ok=%v err=%v", ok, err)
		}
	})

	key, err := secret.NewMasterKey()
	if err != nil {
		t.Fatalf("生成主密钥: %v", err)
	}
	for name, stmt := range map[string]string{
		"加密渠道":     `INSERT INTO channels (name, driver, encrypted) VALUES ('enc', 'local', 1)`,
		"渠道配置密文":   `INSERT INTO channels (name, driver, config_sealed) VALUES ('sealed', 'local', 'x')`,
		"CDN 令牌密文": `INSERT INTO settings (id, cdn_cloudflare_api_token) VALUES (1, 'x')`,
		"上传 Token": `INSERT INTO tokens (user_id, name, token_hash, prefix) VALUES ('` + sqlc.DefaultUserID +
			`', 'picgo', 'h', 'imgy_0000000')`,
	} {
		t.Run("没有指纹却已有"+name, func(t *testing.T) {
			conn, queries := dbtest.New(t)
			res, err := conn.ExecContext(t.Context(), stmt)
			if err != nil {
				t.Fatalf("写入依赖数据: %v", err)
			}
			if n, err := res.RowsAffected(); err != nil || n != 1 {
				t.Fatalf("写入依赖数据影响 %d 行: %v", n, err)
			}
			boot := config.Bootstrap{DataDir: t.TempDir(), MasterKey: key.Hex()}
			if _, err := loadMasterKey(t.Context(), boot, queries, logger); !errors.Is(err, errFingerprintMissing) {
				t.Fatalf("期望 errFingerprintMissing, 得 %v", err)
			}
		})
	}
}
