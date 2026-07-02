package channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/x-haose/imgyoung/internal/db"
	"github.com/x-haose/imgyoung/internal/storage"
)

// ErrInvalidChannel 渠道创建参数非法(名称/后端为空或后端无法初始化)。
var ErrInvalidChannel = errors.New("channel: 渠道参数非法")

// CreateParams 创建渠道的参数。
type CreateParams struct {
	// Name 渠道名(唯一)。
	Name string
	// Driver rclone 后端名。
	Driver string
	// Config 后端参数(含约定键 path / public_base_url),可为 nil。
	Config map[string]string
	// Priority 优先级,越大越优先。
	Priority int64
	// Encrypted 是否为加密渠道;创建时固化,建后不可变。
	Encrypted bool
	// AllowDirectLink 是否允许 302 直链。
	AllowDirectLink bool
}

// Info 渠道的对外描述(不含任何配置与凭证)。
type Info struct {
	// ID 渠道主键。
	ID int64 `json:"id"`
	// Name 渠道名。
	Name string `json:"name"`
	// Driver 后端名。
	Driver string `json:"driver"`
	// Priority 优先级。
	Priority int64 `json:"priority"`
	// Healthy 健康状态。
	Healthy bool `json:"healthy"`
	// Encrypted 是否加密渠道。
	Encrypted bool `json:"encrypted"`
	// AllowDirectLink 是否允许直链。
	AllowDirectLink bool `json:"allow_direct_link"`
}

// Create 创建渠道:先试构造驱动验证配置可用,再落库并重载。
// @param p 创建参数
// @return 新渠道 ID
// @return 参数非法、后端初始化失败或落库失败时的错误
func (m *Manager) Create(ctx context.Context, p CreateParams) (int64, error) {
	if p.Name == "" || p.Driver == "" {
		return 0, fmt.Errorf("%w: name 与 driver 必填", ErrInvalidChannel)
	}
	cfgJSON, err := json.Marshal(p.Config)
	if err != nil {
		return 0, fmt.Errorf("编码渠道配置: %w", err)
	}
	sealed, err := m.cfg.Master.EncryptString(string(cfgJSON))
	if err != nil {
		return 0, fmt.Errorf("加密渠道配置: %w", err)
	}
	toInt := func(b bool) int64 {
		if b {
			return 1
		}
		return 0
	}
	// 试构造:落库前验证后端名与参数能初始化(crypt 派生依赖渠道 ID,
	// 加密渠道以明文形态试构造,包裹层在 Reload 时再生效)。
	trial := db.Channel{Name: p.Name, Driver: p.Driver, ConfigSealed: sealed}
	if _, _, err := m.buildDriver(ctx, trial); err != nil {
		return 0, fmt.Errorf("%w: %w", ErrInvalidChannel, err)
	}

	channelID, err := m.cfg.Queries.CreateChannel(ctx, db.CreateChannelParams{
		Name:            p.Name,
		Driver:          p.Driver,
		ConfigSealed:    sealed,
		Priority:        p.Priority,
		Encrypted:       toInt(p.Encrypted),
		AllowDirectLink: toInt(p.AllowDirectLink),
	})
	if err != nil {
		return 0, fmt.Errorf("写入渠道记录: %w", err)
	}
	if err := m.Reload(ctx); err != nil {
		return 0, err
	}
	return channelID, nil
}

// Delete 删除渠道;若其持有任何图的唯一就绪副本则拒绝(防数据丢失)。
// @param channelID 渠道主键
// @return ErrHoldsSoleReplicas 或删除/重载失败时的错误
func (m *Manager) Delete(ctx context.Context, channelID int64) error {
	sole, err := m.cfg.Queries.CountSoleReplicas(ctx, channelID)
	if err != nil {
		return fmt.Errorf("检查唯一副本: %w", err)
	}
	if sole > 0 {
		return fmt.Errorf("%w(%d 张图仅此渠道有副本)", ErrHoldsSoleReplicas, sole)
	}
	// 先清副本记录(外键约束);渠道上可能残留的对象文件由对账任务回收。
	if err := m.cfg.Queries.DeleteBlobChannelsByChannel(ctx, channelID); err != nil {
		return fmt.Errorf("清理渠道副本记录: %w", err)
	}
	if err := m.cfg.Queries.DeleteChannel(ctx, channelID); err != nil {
		return fmt.Errorf("删除渠道记录: %w", err)
	}
	return m.Reload(ctx)
}

// List 返回全部渠道的对外描述(按优先级降序,不含配置与凭证)。
// @return 渠道描述列表
func (m *Manager) List(ctx context.Context) ([]Info, error) {
	rows, err := m.cfg.Queries.ListChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询渠道列表: %w", err)
	}
	out := make([]Info, 0, len(rows))
	for _, row := range rows {
		out = append(out, Info{
			ID:              row.ID,
			Name:            row.Name,
			Driver:          row.Driver,
			Priority:        row.Priority,
			Healthy:         row.Healthy != 0,
			Encrypted:       row.Encrypted != 0,
			AllowDirectLink: row.AllowDirectLink != 0,
		})
	}
	return out, nil
}

// CheckAll 对全部渠道做一次健康探测,状态变化写回 DB 并重载。
// @Description: 探测 = Stat 一个约定不存在的对象:ErrNotFound 与成功都视为
// 健康(后端可达),其余错误视为不健康(设计定稿·健康检查+自动切换)。
// @return 本次由健康转为不健康(或反向)的渠道名列表
// @return 探测本身无法进行时的错误
func (m *Manager) CheckAll(ctx context.Context) ([]string, error) {
	changed := []string{}
	for _, c := range m.All() {
		healthy := probe(ctx, c.Driver)
		row, err := m.cfg.Queries.GetChannel(ctx, c.ID)
		if err != nil {
			return changed, fmt.Errorf("读取渠道 %q: %w", c.Name, err)
		}
		if (row.Healthy != 0) == healthy {
			continue
		}
		state := int64(0)
		if healthy {
			state = 1
		}
		if err := m.cfg.Queries.UpdateChannelHealth(ctx, db.UpdateChannelHealthParams{
			Healthy: state, ID: c.ID,
		}); err != nil {
			return changed, fmt.Errorf("更新渠道 %q 健康状态: %w", c.Name, err)
		}
		changed = append(changed, c.Name)
	}
	if len(changed) > 0 {
		if err := m.Reload(ctx); err != nil {
			return changed, err
		}
	}
	return changed, nil
}

// probe 探测单个渠道可达性。
// @param d 渠道驱动
// @return 是否健康
func probe(ctx context.Context, d storage.Driver) bool {
	_, err := d.Stat(ctx, ".imgyoung-health-probe")
	return err == nil || errors.Is(err, storage.ErrNotFound)
}
