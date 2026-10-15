package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/secret"
)

// errMasterKeyMismatch 主密钥与库里记下的指纹不符。
var errMasterKeyMismatch = errors.New("主密钥与数据库不符:该库是用另一把主密钥初始化的。" +
	"请换回原来的主密钥(IMGYOUNG_MASTER_KEY 或数据目录下的 master.key);恢复别的实例的快照时,也要换上那个实例的主密钥")

// loadMasterKey 取得主密钥并与库里记下的指纹核对。
// @Description: 环境变量(hex)优先,否则读数据目录下的 master.key。库里没有指纹也没有任何主密钥加密的数据
// (全新实例)时,没有主密钥就生成并落盘,再记下指纹;已有指纹时主密钥必须存在且相符,否则拒绝启动——用新生成或
// 换错的主密钥继续运行,既有渠道配置全部解不开,此后写入的密文又与原有的混用两把密钥。库里没有指纹却已有
// 加密数据(由尚未记录指纹的旧版本写入,如从旧快照恢复)时,同样不生成新密钥,并以能否解开这份数据核对主密钥,
// 解得开才记下指纹。
// @param boot 引导配置
// @param queries 已完成迁移的库
// @return 主密钥
// @return 主密钥非法、缺失、与库不符,或读写失败时的错误
func loadMasterKey(ctx context.Context, boot config.Bootstrap, queries *sqlc.Queries,
	logger *slog.Logger) (*secret.MasterKey, error) {
	stored, err := queries.GetMasterKeyFingerprint(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("读取主密钥指纹: %w", err)
	}
	sealed := ""
	if stored == "" {
		if sealed, err = queries.FirstSealedValue(ctx); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("读取已加密的数据: %w", err)
		}
	}
	key, err := resolveMasterKey(boot, stored != "" || sealed != "", logger)
	if err != nil {
		return nil, err
	}
	if err := checkMasterKey(key, stored, sealed); err != nil {
		return nil, err
	}
	if stored != "" {
		return key, nil
	}
	fp, err := key.Fingerprint()
	if err != nil {
		return nil, err
	}
	if err := queries.InsertMasterKeyFingerprint(ctx, fp); err != nil {
		return nil, fmt.Errorf("记下主密钥指纹: %w", err)
	}
	return key, nil
}

// checkMasterKey 核对主密钥:有指纹按指纹比对,没有指纹而有加密数据时试着解开它。
// @param key 主密钥
// @param stored 库里记下的指纹;空表示没有
// @param sealed 库里任一份主密钥加密的数据;空表示没有
// @return 主密钥与库不符时的 errMasterKeyMismatch;指纹格式非法时的错误
func checkMasterKey(key *secret.MasterKey, stored, sealed string) error {
	if stored != "" {
		ok, err := key.Matches(stored)
		if err != nil {
			return err
		}
		if !ok {
			return errMasterKeyMismatch
		}
		return nil
	}
	if sealed == "" {
		return nil
	}
	if _, err := key.DecryptString(sealed); err != nil {
		return fmt.Errorf("%w(解不开库里已有的加密数据: %w)", errMasterKeyMismatch, err)
	}
	return nil
}

// resolveMasterKey 取得主密钥:环境变量优先,否则数据目录下的文件;只有库还没初始化过主密钥时才生成新的。
// @param boot 引导配置
// @param initialized 库里是否已有主密钥指纹或主密钥加密的数据
// @return 主密钥
// @return 主密钥非法、文件损坏、库已初始化而找不到主密钥,或生成失败时的错误
func resolveMasterKey(boot config.Bootstrap, initialized bool, logger *slog.Logger) (*secret.MasterKey, error) {
	if boot.MasterKey != "" {
		key, err := secret.MasterKeyFromHex(boot.MasterKey)
		if err != nil {
			return nil, fmt.Errorf("IMGYOUNG_MASTER_KEY 非法: %w", err)
		}
		return key, nil
	}
	key, err := secret.LoadFile(boot.DataDir)
	if !errors.Is(err, os.ErrNotExist) {
		return key, err
	}
	path := filepath.Join(boot.DataDir, secret.KeyFileName)
	if initialized {
		return nil, fmt.Errorf("数据库已用主密钥加密过数据,但找不到主密钥:IMGYOUNG_MASTER_KEY 未设置,%s 也不存在。"+
			"请恢复原来的主密钥再启动;生成新密钥会让渠道配置与加密渠道的数据无法解密", path)
	}
	key, err = secret.CreateFile(boot.DataDir)
	if err != nil {
		return nil, err
	}
	logger.Warn("已生成主密钥文件,请立即备份——丢失将导致渠道配置与加密渠道数据永久不可恢复", "path", path)
	return key, nil
}
