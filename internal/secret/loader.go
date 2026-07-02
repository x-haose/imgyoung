package secret

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// keyFileName 数据目录下的主密钥文件名。
const keyFileName = "master.key"

// LoadOrCreate 从数据目录加载主密钥文件,不存在则生成并落盘(0600)。
// @Description: 环境变量方式(IMGYOUNG_MASTER_KEY)优先于本函数,由装配层决定;
// 文件损坏时报错而非静默重建——重建会让全部既有密文作废。
// @param dataDir 数据目录
// @return 主密钥
// @return 是否为本次新建
// @return 读取/生成/写入失败时的错误
func LoadOrCreate(dataDir string) (*MasterKey, bool, error) {
	path := filepath.Join(dataDir, keyFileName)

	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		key, kerr := MasterKeyFromHex(strings.TrimSpace(string(raw)))
		if kerr != nil {
			return nil, false, fmt.Errorf("主密钥文件 %q 损坏(勿删除重建,否则全部密文作废): %w", path, kerr)
		}
		return key, false, nil
	case errors.Is(err, os.ErrNotExist):
		key, kerr := NewMasterKey()
		if kerr != nil {
			return nil, false, kerr
		}
		// 临时文件 + 重命名保证原子性:写一半崩溃不会留下损坏的密钥文件。
		tmp := path + ".tmp"
		if werr := os.WriteFile(tmp, []byte(key.Hex()+"\n"), 0o600); werr != nil {
			return nil, false, fmt.Errorf("写入主密钥临时文件 %q: %w", tmp, werr)
		}
		if werr := os.Rename(tmp, path); werr != nil {
			return nil, false, fmt.Errorf("落定主密钥文件 %q: %w", path, werr)
		}
		return key, true, nil
	default:
		return nil, false, fmt.Errorf("读取主密钥文件 %q: %w", path, err)
	}
}
