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
// @Description: 环境变量(hex)优先,否则读数据目录下的 master.key。库里有指纹时主密钥必须存在且相符,否则拒绝启动
// ——用新生成或换错的主密钥继续运行,既有渠道配置全部解不开,此后写入的密文又与原有的混用两把密钥。库里没有指纹
// (全新实例,或尚未记录指纹的旧版本写下的库,如从旧快照恢复)时看库里有没有依赖主密钥的数据(见 keyDependents):
// 有就不生成新密钥,有密文时以能否解开任意一份核对主密钥,只有派生数据时无从核对、照用且不记指纹;都没有才是
// 全新实例,没有主密钥就生成并落盘。核对过的主密钥记下指纹。
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
	var deps keyDependents
	if stored == "" {
		if deps, err = loadKeyDependents(ctx, queries); err != nil {
			return nil, err
		}
	}
	key, err := resolveMasterKey(boot, stored != "" || deps.exist(), logger)
	if err != nil {
		return nil, err
	}
	if err := checkMasterKey(key, stored, deps.sealed); err != nil {
		return nil, err
	}
	if stored != "" {
		return key, nil
	}
	if len(deps.sealed) == 0 && deps.derived {
		// 无从核对就不记指纹:记下的若是一把错钥,换回原来的那把反而被拒;库里有了密文后下次启动再核对、记录。
		logger.Warn("库里没有可核对的加密数据,暂不记录主密钥指纹;上传 Token 的 S3 凭证若在客户端失效,说明主密钥不是原来那把")
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

// keyDependents 库里依赖主密钥的数据。
type keyDependents struct {
	sealed  []string // 主密钥加密的密文:渠道配置、CDN 令牌
	derived bool     // 是否有由主密钥派生、不落密文的数据:上传 Token 的 S3 密钥、加密渠道的 crypt 密钥
}

// exist 报告库里是否有任何依赖主密钥的数据。
// @return 有为 true
func (d keyDependents) exist() bool {
	return len(d.sealed) > 0 || d.derived
}

// loadKeyDependents 读出库里依赖主密钥的数据。
// @param queries 已完成迁移的库
// @return 依赖主密钥的数据
// @return 查询失败时的错误
func loadKeyDependents(ctx context.Context, queries *sqlc.Queries) (keyDependents, error) {
	sealed, err := queries.ListSealedValues(ctx)
	if err != nil {
		return keyDependents{}, fmt.Errorf("读取已加密的数据: %w", err)
	}
	derived, err := queries.HasMasterKeyDerivedData(ctx)
	if err != nil {
		return keyDependents{}, fmt.Errorf("读取由主密钥派生的数据: %w", err)
	}
	return keyDependents{sealed: sealed, derived: derived.Bool}, nil
}

// checkMasterKey 核对主密钥:有指纹按指纹比对,没有指纹而有密文时能解开任意一份即为相符。
// @Description: 旧版本在 master.key 丢失时会生成新密钥,这样的库里混有两把密钥的密文,只看某一份会拒绝正确的密钥。
// @param key 主密钥
// @param stored 库里记下的指纹;空表示没有
// @param sealed 库里主密钥加密的密文;空表示没有,此时无从核对
// @return 主密钥与库不符时的 errMasterKeyMismatch;指纹格式非法时的错误
func checkMasterKey(key *secret.MasterKey, stored string, sealed []string) error {
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
	if len(sealed) == 0 {
		return nil
	}
	var lastErr error
	for _, v := range sealed {
		if _, lastErr = key.DecryptString(v); lastErr == nil {
			return nil
		}
	}
	return fmt.Errorf("%w(库里已有的 %d 份加密数据都解不开: %w)", errMasterKeyMismatch, len(sealed), lastErr)
}

// resolveMasterKey 取得主密钥:环境变量优先,否则数据目录下的文件;只有库还没初始化过主密钥时才生成新的。
// @param boot 引导配置
// @param initialized 库里是否已有主密钥指纹或依赖主密钥的数据
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
