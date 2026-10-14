package channel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SecretPlaceholder 敏感配置值的回读哨兵:配置回读时替代真实凭证下发,
// 编辑提交时回传该值表示保留渠道已存的同键原值,凭证明文不经前端往返。
const SecretPlaceholder = "__imgyoung_secret_keep__" //nolint:gosec // 公开的掩码哨兵字面量,不是凭证

// EditConfig 渠道配置的编辑视图(敏感值已哨兵化,供编辑表单预填)。
type EditConfig struct {
	Template string            `json:"template"`         // 配置所用模板 id;空 = 自定义配置
	Driver   string            `json:"driver"`           // rclone 后端名
	Config   map[string]string `json:"config"`           // 解密后的配置(含约定键 path / public_base_url),敏感键值以哨兵掩码
	Inputs   map[string]string `json:"inputs,omitempty"` // 模板表单字段值(由 config 反解,密码字段哨兵掩码);仅模板渠道
}

// ConfigForEdit 返回渠道配置的编辑视图。
// @Description: 敏感键(名称含 key/secret/password/token/credential)的值以
// SecretPlaceholder 掩码;模板渠道额外反解出表单字段值供还原模板表单。
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
	out := EditConfig{Template: row.Template, Driver: row.Driver, Config: maskSecrets(params)}
	if row.Template != "" {
		if inputs, ok := TemplateInputsFromConfig(row.Template, params); ok {
			out.Inputs = inputs
		}
	}
	return out, nil
}

// RestoreSecrets 把配置中的哨兵值还原为指定渠道已存配置的同键原值。
// @Description: 供编辑场景"先测后存"复用——表单回传的掩码凭证在探测前还原,
// 与 Update 的还原规则一致。
// @param channelID 渠道主键
// @param config 待还原的配置
// @return 还原后的新配置(不修改入参)
// @return ErrChannelNotFound、解密失败或哨兵无原值可还原时的错误
func (m *Manager) RestoreSecrets(ctx context.Context, channelID int64,
	config map[string]string) (map[string]string, error) {
	row, err := m.cfg.Queries.GetChannel(ctx, channelID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrChannelNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("读取渠道 %d: %w", channelID, err)
	}
	old, err := m.unsealConfigMap(row.ConfigSealed)
	if err != nil {
		return nil, err
	}
	return restoreSecrets(config, old)
}

// maskSecrets 返回把敏感键值替换为 SecretPlaceholder 的配置副本。
// @param config 明文配置
// @return 掩码后的新配置(不修改入参)
func maskSecrets(config map[string]string) map[string]string {
	out := make(map[string]string, len(config))
	for k, v := range config {
		if v != "" && secretKeyPattern.MatchString(k) {
			out[k] = SecretPlaceholder
			continue
		}
		out[k] = v
	}
	return out
}

// restoreSecrets 把 config 中等于 SecretPlaceholder 的值替换为 old 的同键原值。
// @param config 提交的配置,可为 nil
// @param old 渠道已存的明文配置
// @return 还原后的新配置(不修改入参)
// @return 哨兵键在 old 中无原值时的 ErrInvalidChannel(此时只能重新填写)
func restoreSecrets(config, old map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(config))
	for k, v := range config {
		if v != SecretPlaceholder {
			out[k] = v
			continue
		}
		ov, ok := old[k]
		if !ok || ov == "" {
			return nil, fmt.Errorf("%w: 字段 %q 无已存原值可保留,请重新填写", ErrInvalidChannel, k)
		}
		out[k] = ov
	}
	return out, nil
}
