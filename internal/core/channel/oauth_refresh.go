package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"golang.org/x/oauth2"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// tokenRefreshLead 令牌剩余寿命低于此值即提前刷新,留足重试余量。
const tokenRefreshLead = 30 * time.Minute

// RefreshOAuthTokens 刷新各网盘渠道临期的授权令牌并落库。
// @Description: 凭证内联注入连接字符串,rclone 刷新后写回的是临时配置映射,进程重启即
// 丢失;不落库会一直退回初始刷新令牌,对刷新即轮换的服务商最终掉线。故由本方法主动
// 刷新并加密写回,随后重载以失效持有旧令牌的 fs 缓存。
// 单渠道失败只记录不中断整轮——一个渠道的授权失效不应拖垮其余渠道的续期。
// @param ctx 控制整轮的超时与取消
// @return 实际完成刷新的渠道名列表
// @return 渠道列表读取失败时的错误
func (m *Manager) RefreshOAuthTokens(ctx context.Context) ([]string, error) {
	rows, err := m.cfg.Queries.ListChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取渠道列表: %w", err)
	}
	refreshed := []string{}
	for _, row := range rows {
		ok, err := m.refreshChannelToken(ctx, row)
		if err != nil {
			m.cfg.Logger.Warn("刷新渠道授权令牌失败", "channel", row.Name, "err", err)
			continue
		}
		if ok {
			refreshed = append(refreshed, row.Name)
		}
	}
	if len(refreshed) > 0 {
		if err := m.Reload(ctx); err != nil {
			return refreshed, fmt.Errorf("令牌已刷新但重载失败: %w", err)
		}
	}
	return refreshed, nil
}

// refreshChannelToken 刷新单个渠道的授权令牌。
// @param ctx 控制刷新请求的超时与取消
// @param row 渠道记录
// @return 是否实际刷新并落库
// @return 解密、刷新或落库失败时的错误
func (m *Manager) refreshChannelToken(ctx context.Context, row sqlc.Channel) (bool, error) {
	config, err := m.unsealConfigMap(row.ConfigSealed)
	if err != nil {
		return false, err
	}
	raw := config[ConfigKeyToken]
	if raw == "" {
		return false, nil // 非授权类渠道
	}
	var tok oauth2.Token
	if err := json.Unmarshal([]byte(raw), &tok); err != nil {
		return false, fmt.Errorf("解析已存令牌: %w", err)
	}
	if !needsRefresh(&tok) {
		return false, nil
	}

	flow, err := NewOAuthFlow(ctx, row.Driver, config, "")
	if err != nil {
		return false, err
	}
	fresh, err := flow.Refresh(ctx, &tok)
	if err != nil {
		return false, err
	}
	if fresh.AccessToken == tok.AccessToken {
		return false, nil // 服务商判定旧令牌仍有效
	}

	updated, err := marshalToken(fresh)
	if err != nil {
		return false, err
	}
	config[ConfigKeyToken] = updated
	sealed, err := m.sealConfigMap(config)
	if err != nil {
		return false, err
	}
	if err := m.cfg.Queries.UpdateChannelConfig(ctx, sqlc.UpdateChannelConfigParams{
		Name: row.Name, Driver: row.Driver, ConfigSealed: sealed, Priority: row.Priority,
		AllowDirectLink: row.AllowDirectLink, Template: row.Template, ID: row.ID,
	}); err != nil {
		return false, fmt.Errorf("写回令牌: %w", err)
	}
	return true, nil
}

// needsRefresh 判定令牌是否已临期。
// @Description: 到期时刻为零值视为不过期(部分服务商签发长期令牌),不做无谓刷新。
// @param tok 令牌
// @return 是否需要刷新
func needsRefresh(tok *oauth2.Token) bool {
	if tok.RefreshToken == "" {
		return false // 无从刷新
	}
	if tok.Expiry.IsZero() {
		return false
	}
	return time.Until(tok.Expiry) < tokenRefreshLead
}
