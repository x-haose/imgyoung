package secret

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testKey 生成一把测试主密钥。
func testKey(t *testing.T) *MasterKey {
	t.Helper()
	k, err := NewMasterKey()
	if err != nil {
		t.Fatalf("NewMasterKey 返回错误: %v", err)
	}
	return k
}

// TestEncryptDecryptRoundTrip 验证加密后可解回原文,且密文不含明文。
func TestEncryptDecryptRoundTrip(t *testing.T) {
	k := testKey(t)
	plain := `{"access_key_id":"AKIA_FAKE_FOR_TEST","bucket":"imgs"}`

	sealed, err := k.EncryptString(plain)
	if err != nil {
		t.Fatalf("EncryptString 返回错误: %v", err)
	}
	if strings.Contains(sealed, "AKIA_FAKE_FOR_TEST") {
		t.Fatal("密文中出现明文片段")
	}
	got, err := k.DecryptString(sealed)
	if err != nil {
		t.Fatalf("DecryptString 返回错误: %v", err)
	}
	if got != plain {
		t.Fatalf("解密结果 = %q, 期望原文", got)
	}
}

// TestEncryptNonDeterministic 验证同一明文两次加密产生不同密文(随机 nonce)。
func TestEncryptNonDeterministic(t *testing.T) {
	k := testKey(t)
	a, err := k.EncryptString("same-plaintext")
	if err != nil {
		t.Fatalf("EncryptString 返回错误: %v", err)
	}
	b, err := k.EncryptString("same-plaintext")
	if err != nil {
		t.Fatalf("EncryptString 返回错误: %v", err)
	}
	if a == b {
		t.Fatal("两次加密密文相同,nonce 未随机化")
	}
}

// TestDecryptRejectsTampered 验证密文被篡改后解密失败(GCM 认证)。
func TestDecryptRejectsTampered(t *testing.T) {
	k := testKey(t)
	sealed, err := k.EncryptString("secret")
	if err != nil {
		t.Fatalf("EncryptString 返回错误: %v", err)
	}
	tampered := []byte(sealed)
	tampered[len(tampered)-1] ^= 0x01
	if _, err := k.DecryptString(string(tampered)); err == nil {
		t.Fatal("篡改后的密文应解密失败")
	}
}

// TestDecryptRejectsWrongKey 验证错误密钥无法解密。
func TestDecryptRejectsWrongKey(t *testing.T) {
	sealed, err := testKey(t).EncryptString("secret")
	if err != nil {
		t.Fatalf("EncryptString 返回错误: %v", err)
	}
	if _, err := testKey(t).DecryptString(sealed); err == nil {
		t.Fatal("错误密钥应解密失败")
	}
}

// TestMasterKeyHexRoundTrip 验证主密钥可导出为 hex 并等价还原。
func TestMasterKeyHexRoundTrip(t *testing.T) {
	k := testKey(t)
	restored, err := MasterKeyFromHex(k.Hex())
	if err != nil {
		t.Fatalf("MasterKeyFromHex 返回错误: %v", err)
	}
	sealed, err := k.EncryptString("cross-instance")
	if err != nil {
		t.Fatalf("EncryptString 返回错误: %v", err)
	}
	got, err := restored.DecryptString(sealed)
	if err != nil || got != "cross-instance" {
		t.Fatalf("还原密钥解密失败: %v, got=%q", err, got)
	}
}

// TestMasterKeyFromHexRejectsInvalid 验证非法 hex 或长度错误被拒绝。
func TestMasterKeyFromHexRejectsInvalid(t *testing.T) {
	for name, v := range map[string]string{"非hex": "zz", "长度不足": "abcd12"} {
		t.Run(name, func(t *testing.T) {
			if _, err := MasterKeyFromHex(v); err == nil {
				t.Fatal("非法主密钥应返回错误")
			}
		})
	}
}

// TestLoadOrCreateFirstRun 验证首次调用生成密钥文件(0600)并可用。
func TestLoadOrCreateFirstRun(t *testing.T) {
	dir := t.TempDir()
	k, created, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate 返回错误: %v", err)
	}
	if !created {
		t.Fatal("首次调用应报告新建")
	}
	info, err := os.Stat(filepath.Join(dir, keyFileName))
	if err != nil {
		t.Fatalf("密钥文件未生成: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("密钥文件权限 = %o, 期望 0600", info.Mode().Perm())
	}
	if k == nil {
		t.Fatal("返回的主密钥为空")
	}
}

// TestLoadOrCreateSecondRun 验证再次调用加载同一把密钥。
func TestLoadOrCreateSecondRun(t *testing.T) {
	dir := t.TempDir()
	first, _, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("首次 LoadOrCreate: %v", err)
	}
	second, created, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("二次 LoadOrCreate: %v", err)
	}
	if created {
		t.Fatal("二次调用不应新建")
	}
	if first.Hex() != second.Hex() {
		t.Fatal("两次加载的密钥不一致")
	}
}

// TestLoadOrCreateRejectsCorrupted 验证密钥文件损坏时报错而非静默重建。
func TestLoadOrCreateRejectsCorrupted(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, keyFileName), []byte("corrupted"), 0o600); err != nil {
		t.Fatalf("写损坏文件: %v", err)
	}
	if _, _, err := LoadOrCreate(dir); err == nil {
		t.Fatal("损坏的密钥文件应报错(静默重建会让全部密文作废)")
	}
}

// TestDeriveKey 验证派生密钥:确定性、按用途隔离、不等于主密钥。
func TestDeriveKey(t *testing.T) {
	k := testKey(t)
	a1, err := k.Derive("channel:1", 32)
	if err != nil {
		t.Fatalf("Derive 返回错误: %v", err)
	}
	a2, err := k.Derive("channel:1", 32)
	if err != nil {
		t.Fatalf("Derive 返回错误: %v", err)
	}
	b, err := k.Derive("channel:2", 32)
	if err != nil {
		t.Fatalf("Derive 返回错误: %v", err)
	}
	if !bytes.Equal(a1, a2) {
		t.Fatal("同一用途两次派生结果不一致")
	}
	if bytes.Equal(a1, b) {
		t.Fatal("不同用途派生出相同密钥,隔离失效")
	}
	if len(a1) != 32 {
		t.Fatalf("派生长度 = %d, 期望 32", len(a1))
	}
}
