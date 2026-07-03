// Package secret 提供主密钥管理:渠道凭证的静态加密(AES-GCM)与
// 按用途派生子密钥(HKDF)。主密钥丢失 = 全部密文作废。
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

// keyLen 主密钥字节长度(AES-256)。
const keyLen = 32

// MasterKey 进程持有的主密钥,所有静态加密与派生的根。
type MasterKey struct {
	key []byte // 原始密钥字节
}

// NewMasterKey 生成一把新的随机主密钥。
// @return 新主密钥
// @return 随机源不可用时的错误
func NewMasterKey() (*MasterKey, error) {
	key := make([]byte, keyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("读取随机源: %w", err)
	}
	return &MasterKey{key: key}, nil
}

// MasterKeyFromHex 从十六进制字符串还原主密钥。
// @param s 64 位 hex 字符串(32 字节)
// @return 还原的主密钥
// @return 非法 hex 或长度错误时的错误
func MasterKeyFromHex(s string) (*MasterKey, error) {
	key, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("主密钥不是合法 hex: %w", err)
	}
	if len(key) != keyLen {
		return nil, fmt.Errorf("主密钥长度 %d 字节, 需要 %d 字节", len(key), keyLen)
	}
	return &MasterKey{key: key}, nil
}

// Hex 导出主密钥的十六进制表示(用于首启提示用户保存)。
// @return 64 位 hex 字符串
func (k *MasterKey) Hex() string {
	return hex.EncodeToString(k.key)
}

// EncryptString 用主密钥加密明文,返回可入库的编码密文。
// @Description: AES-256-GCM,随机 nonce 前置;密文含认证标签,篡改可检。
// @param plain 明文
// @return base64 编码的密文
// @return 加密失败时的错误
func (k *MasterKey) EncryptString(plain string) (string, error) {
	aead, err := k.aead()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("读取随机源: %w", err)
	}
	sealed := aead.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// DecryptString 解密 EncryptString 产出的密文。
// @param sealed base64 编码的密文
// @return 明文
// @return 密文非法、被篡改或密钥不符时的错误
func (k *MasterKey) DecryptString(sealed string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", fmt.Errorf("密文不是合法 base64: %w", err)
	}
	aead, err := k.aead()
	if err != nil {
		return "", err
	}
	if len(raw) < aead.NonceSize() {
		return "", errors.New("密文长度不足")
	}
	nonce, body := raw[:aead.NonceSize()], raw[aead.NonceSize():]
	plain, err := aead.Open(nil, nonce, body, nil)
	if err != nil {
		return "", fmt.Errorf("解密失败(密钥不符或密文被篡改): %w", err)
	}
	return string(plain), nil
}

// Derive 按用途从主密钥派生子密钥(HKDF-SHA256)。
// @Description: 同一用途派生结果确定;不同用途互相隔离,单个子密钥
// 泄露不扩散。
// @param purpose 用途标识(如 "channel:3")
// @param n 派生字节数
// @return 派生密钥
// @return 派生失败时的错误
func (k *MasterKey) Derive(purpose string, n int) ([]byte, error) {
	out, err := hkdf.Key(sha256.New, k.key, nil, purpose, n)
	if err != nil {
		return nil, fmt.Errorf("派生子密钥(%s): %w", purpose, err)
	}
	return out, nil
}

// aead 构造 AES-256-GCM 实例。
// @return 就绪的 AEAD
// @return 密钥非法时的错误
func (k *MasterKey) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(k.key)
	if err != nil {
		return nil, fmt.Errorf("初始化 AES: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("初始化 GCM: %w", err)
	}
	return aead, nil
}
