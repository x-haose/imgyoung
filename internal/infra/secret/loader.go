package secret

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// KeyFileName 数据目录下的主密钥文件名。
const KeyFileName = "master.key"

// LoadFile 读数据目录下的主密钥文件。
// @Description: 文件损坏时报错而非重建——重建会让全部既有密文作废。
// @param dataDir 数据目录
// @return 主密钥
// @return 文件不存在时包装 os.ErrNotExist;损坏或读取失败时的错误
func LoadFile(dataDir string) (*MasterKey, error) {
	path := filepath.Join(dataDir, KeyFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取主密钥文件 %q: %w", path, err)
	}
	key, err := MasterKeyFromHex(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("主密钥文件 %q 损坏(勿删除重建,否则全部密文作废): %w", path, err)
	}
	return key, nil
}

// CreateFile 生成一把新主密钥并写入数据目录(0600)。
// @Description: 先写临时文件再重命名,写一半崩溃不会留下损坏的密钥文件。
// @param dataDir 数据目录
// @return 新主密钥
// @return 生成或写入失败时的错误
func CreateFile(dataDir string) (*MasterKey, error) {
	key, err := NewMasterKey()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, KeyFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(key.Hex()+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("写入主密钥临时文件 %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return nil, fmt.Errorf("落定主密钥文件 %q: %w", path, err)
	}
	return key, nil
}
