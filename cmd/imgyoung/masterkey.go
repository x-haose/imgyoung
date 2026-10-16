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

// errFingerprintMissing 库里没有主密钥指纹,却已有依赖主密钥的数据。
// @Description: 指纹在全新实例首次启动时记下,早于任何依赖主密钥的数据写入;出现这种组合说明库的状态不对,
// 无从核对主密钥,继续运行可能让新旧两把密钥的数据混在一起。
var errFingerprintMissing = errors.New("数据库缺少主密钥指纹,却已有依赖主密钥的数据(渠道配置、加密渠道、上传 Token 或 CDN 令牌)," +
	"无法核对主密钥,拒绝启动")

// loadMasterKey 取得主密钥并与库里记下的指纹核对。
// @Description: 环境变量(hex)优先,否则读数据目录下的 master.key。库里有指纹时主密钥必须存在且相符,否则拒绝启动
// ——用新生成或换错的主密钥继续运行,既有渠道配置全部解不开,此后写入的密文又与原有的混用两把密钥。
// 没有指纹的是全新实例:没有主密钥就生成并落盘,随后记下指纹;没有指纹却已有依赖主密钥的数据时拒绝启动。
// @param boot 引导配置
// @param queries 已完成迁移的库
// @return 主密钥
// @return 主密钥非法、缺失、与库不符,库缺少指纹却已有依赖数据,或读写失败时的错误
func loadMasterKey(ctx context.Context, boot config.Bootstrap, queries *sqlc.Queries,
	logger *slog.Logger) (*secret.MasterKey, error) {
	stored, err := queries.GetMasterKeyFingerprint(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("读取主密钥指纹: %w", err)
	}
	if stored != "" {
		key, err := resolveMasterKey(boot, true, logger)
		if err != nil {
			return nil, err
		}
		ok, err := key.Matches(stored)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errMasterKeyMismatch
		}
		return key, nil
	}
	dependents, err := queries.HasMasterKeyDependents(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取依赖主密钥的数据: %w", err)
	}
	if dependents.Bool {
		return nil, errFingerprintMissing
	}
	key, err := resolveMasterKey(boot, false, logger)
	if err != nil {
		return nil, err
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

// resolveMasterKey 取得主密钥:环境变量优先,否则数据目录下的文件;只有库还没初始化过主密钥时才生成新的。
// @param boot 引导配置
// @param initialized 库里是否已记下主密钥指纹
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
		return nil, fmt.Errorf("数据库里已有依赖主密钥的数据,但找不到主密钥:IMGYOUNG_MASTER_KEY 未设置,%s 也不存在。"+
			"请恢复原来的主密钥再启动;生成新密钥会让渠道配置、加密渠道的数据与上传 Token 的 S3 凭证全部失效", path)
	}
	key, err = secret.CreateFile(boot.DataDir)
	if err != nil {
		return nil, err
	}
	logger.Warn("已生成主密钥文件,请立即备份——丢失将导致渠道配置与加密渠道数据永久不可恢复", "path", path)
	return key, nil
}
