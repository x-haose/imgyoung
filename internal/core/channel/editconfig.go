package channel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/x-haose/imgyoung/store/rclone"
)

// SecretPlaceholder 敏感配置值的回读哨兵:配置回读时替代真实凭证下发,
// 编辑提交时回传该值表示保留渠道已存的同键原值,凭证明文不经前端往返。
const SecretPlaceholder = "__imgyoung_secret_keep__" //nolint:gosec // 公开的掩码哨兵字面量,不是凭证

// EditConfig 渠道配置的编辑视图(敏感值已哨兵化,供编辑表单预填)。
type EditConfig struct {
	Template string            `json:"template"`         // 配置所用模板 id;空 = 自定义配置
	Driver   string            `json:"driver"`           // rclone 后端名
	Config   map[string]string `json:"config"`           // 解密后的配置(含约定键 path / public_base_url),敏感键值以哨兵掩码
	Inputs   map[string]string `json:"inputs,omitempty"` // 模板表单字段值(由 config 反解,敏感字段哨兵掩码);仅模板渠道
}

// ConfigForEdit 返回渠道配置的编辑视图。
// @Description: 敏感键(见 secretKeysOf)的值以 SecretPlaceholder 掩码;模板渠道额外反解出
// 表单字段值供还原模板表单,同一口径掩码。
// @param channelID 渠道主键
// @return 编辑视图
// @return ErrChannelNotFound 或查询、解密失败时的错误
func (m *Manager) ConfigForEdit(ctx context.Context, channelID int64) (EditConfig, error) {
	row, err := m.cfg.Queries.GetChannel(ctx, channelID)
	if errors.Is(err, sql.ErrNoRows) {
		return EditConfig{}, ErrChannelNotFound
	}
	if err != nil {
		return EditConfig{}, fmt.Errorf("读取渠道 %d: %w", channelID, err)
	}
	params, err := m.unsealConfigMap(row.ConfigSealed)
	if err != nil {
		return EditConfig{}, err
	}
	secret, err := secretKeysOf(row.Driver)
	if err != nil {
		return EditConfig{}, err
	}
	out := EditConfig{Template: row.Template, Driver: row.Driver, Config: maskSecrets(params, secret)}
	if row.Template != "" {
		if inputs, ok := TemplateInputsFromConfig(row.Template, params, secret); ok {
			out.Inputs = inputs
		}
	}
	return out, nil
}

// storedConfig 读取渠道已存的明文配置,供编辑场景还原哨兵。
// @param channelID 渠道主键
// @return 明文配置
// @return ErrChannelNotFound 或查询、解密失败时的错误
func (m *Manager) storedConfig(ctx context.Context, channelID int64) (map[string]string, error) {
	row, err := m.cfg.Queries.GetChannel(ctx, channelID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrChannelNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("读取渠道 %d: %w", channelID, err)
	}
	return m.unsealConfigMap(row.ConfigSealed)
}

// secretKeysOf 返回渠道配置里值须保密的键:后端声明为密码或敏感信息的参数(rclone.SensitiveParams)。
// @Description: 约定键 path / public_base_url 不交给后端,不属保密范围;OAuth 后端的 client_id、
// client_secret 与 token 由后端声明为敏感,在其中。
// @param driver rclone 后端名
// @return 键集合
// @return 后端不存在时的 ErrInvalidChannel
func secretKeysOf(driver string) (map[string]bool, error) {
	names, err := rclone.SensitiveParams(driver)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidChannel, err)
	}
	secret := make(map[string]bool, len(names))
	for _, n := range names {
		secret[n] = true
	}
	delete(secret, configKeyPath)
	delete(secret, configKeyPublicBaseURL)
	return secret, nil
}

// maskSecrets 返回把敏感键值替换为 SecretPlaceholder 的配置副本。
// @param config 明文配置
// @param secret 须保密的键(见 secretKeysOf)
// @return 掩码后的新配置(不修改入参)
func maskSecrets(config map[string]string, secret map[string]bool) map[string]string {
	out := make(map[string]string, len(config))
	for k, v := range config {
		if v != "" && secret[k] {
			out[k] = SecretPlaceholder
			continue
		}
		out[k] = v
	}
	return out
}
