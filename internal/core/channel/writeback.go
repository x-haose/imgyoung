package channel

import (
	"context"
	"fmt"
	"maps"
	"sync"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// writeBacks 收集一次性驱动的写回,关闭驱动后并入将要落库的配置。
type writeBacks struct {
	mu     sync.Mutex        // 后端可能在任意 goroutine 写回
	values map[string]string // 参数名 → 最近一次写回的值
}

// record 记下一次写回;签名与 rclone.Options.OnChange 一致。
// @param key 参数名
// @param value 新值
func (w *writeBacks) record(key, value string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.values == nil {
		w.values = map[string]string{}
	}
	w.values[key] = value
}

// applyTo 把收集到的写回并入配置(原地修改)。
// @param config 将要落库的配置
func (w *writeBacks) applyTo(config map[string]string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	maps.Copy(config, w.values)
}

// liveParams 一个驱动手里当前的参数值:构造时的配置,随后端每次写回更新。
// @Description: 写回落库前拿它核对库里的值——库里已不是驱动手里的旧值,说明配置在驱动构造之后被编辑过,
// 这个驱动的写回属于旧配置,不得覆盖。
type liveParams struct {
	mu     sync.Mutex        // 后端可能在任意 goroutine 写回
	values map[string]string // 参数名 → 驱动当前的值
}

// newLiveParams 以构造驱动所用的配置建立参数值表。
// @param params 构造驱动所用的完整配置
// @return 参数值表(持有副本)
func newLiveParams(params map[string]string) *liveParams {
	return &liveParams{values: maps.Clone(params)}
}

// swap 记下参数的新值,返回此前驱动手里的值。
// @param key 参数名
// @param value 新值
// @return 旧值;此前没有该参数时为空串
func (l *liveParams) swap(key, value string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	old := l.values[key]
	l.values[key] = value
	return old
}

// snapshot 返回当前参数值的副本。
// @return 参数名 → 值
func (l *liveParams) snapshot() map[string]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return maps.Clone(l.values)
}

// persistParam 把后端运行期写回的参数(刷新后的 OAuth 令牌等)并入渠道配置并落库,前提是库里仍是写回之前的值。
// @Description: 写回来自 rclone 内部,不带上下文,也不该被某个请求的取消牵连,故用独立上下文。
// 落库失败只能记录——rclone 的写回接口无处接收错误;内存里的新值仍在用,重启后退回旧值。
// @param channelID 渠道主键
// @param key 参数名
// @param expected 写回之前驱动手里的值
// @param value 新值
// @return 库里是否已是该值(本次写入或原本相同);库里已被改成别的值或落库失败时为 false,原因已记录
func (m *Manager) persistParam(channelID int64, key, expected, value string) bool {
	applied, err := m.mergeParam(context.Background(), channelID, key, expected, value)
	if err != nil {
		m.cfg.Logger.Error("后端写回参数无法落库", "channel_id", channelID, "key", key, "err", err)
		return false
	}
	if !applied {
		m.cfg.Logger.Info("渠道配置已被编辑,丢弃旧驱动的参数写回", "channel_id", channelID, "key", key)
	}
	return applied
}

// mergeAttempts 并入一个参数的最多尝试次数;每次落空都说明读到配置之后另有一次配置写入先落了库。
const mergeAttempts = 3

// mergeParam 把一个参数并入渠道配置并落库,前提是库里该参数仍是 expected。
// @Description: 配置密文整体读出、改一个参数、整体写回;写回以读到的密文为条件,其间配置被别处写过则重读重来,
// 不覆盖别人的写入。
// @param channelID 渠道主键
// @param key 参数名
// @param expected 库里应有的旧值;不符说明配置已被编辑,不写
// @param value 新值
// @return applied 库里是否已是 value(本次写入或原本相同)
// @return err 读取、解密、加密或落库失败,或配置连续被改动、尝试用尽时的错误
func (m *Manager) mergeParam(ctx context.Context, channelID int64, key, expected, value string) (applied bool,
	err error) {
	for range mergeAttempts {
		row, err := m.cfg.Queries.GetChannel(ctx, channelID)
		if err != nil {
			return false, fmt.Errorf("读取渠道 %d: %w", channelID, err)
		}
		params, err := m.unsealConfigMap(row.ConfigSealed)
		if err != nil {
			return false, err
		}
		switch params[key] {
		case value:
			return true, nil
		case expected:
		default:
			return false, nil
		}
		params[key] = value
		sealed, err := m.sealConfigMap(params)
		if err != nil {
			return false, err
		}
		n, err := m.cfg.Queries.UpdateChannelConfigSealed(ctx, sqlc.UpdateChannelConfigSealedParams{
			ConfigSealed: sealed, ID: channelID, ExpectedSealed: row.ConfigSealed,
		})
		if err != nil {
			return false, fmt.Errorf("写入渠道 %q 配置: %w", row.Name, err)
		}
		if n == 1 {
			m.cfg.Logger.Info("渠道参数已由后端写回并落库", "channel", row.Name, "key", key)
			return true, nil
		}
	}
	return false, fmt.Errorf("渠道 %d 的配置在并入参数期间连续被改动 %d 次", channelID, mergeAttempts)
}

// persistOwn 落库渠道自己驱动的写回,并同步进该驱动对应的驱动输入。
// @Description: 驱动手里已是新值,下次 Reload 不应把写回误判为配置变更而重建驱动。写回只同步给构造出这个
// 驱动的那条运行态;配置已被编辑时(库里已不是驱动手里的旧值)写回丢弃,驱动随后被 Reload 换掉。
// 一次性驱动的写回只经 persistParam 落库、不同步,下次 Reload 据此重建在用驱动,换用新值。
// @param channelID 渠道主键
// @param live 写回所属驱动的参数值表
// @param key 参数名
// @param value 新值
func (m *Manager) persistOwn(channelID int64, live *liveParams, key, value string) {
	if !m.persistParam(channelID, key, live.swap(key, value), value) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.channels {
		if m.channels[i].live == live {
			next := maps.Clone(m.channels[i].source.params)
			next[key] = value
			m.channels[i].source.params = next
		}
	}
}
