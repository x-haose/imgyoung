package channel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/store/driver"
)

// ErrInvalidChannel 渠道创建参数非法(名称/后端为空或后端无法初始化)。
var ErrInvalidChannel = errors.New("channel: 渠道参数非法")

// ErrChannelNameExists 渠道名已存在(唯一约束冲突);由 Create 收敛底层 DB 错误得来,
// 调用方以 errors.Is 判定,不必自行匹配驱动错误文本。
var ErrChannelNameExists = errors.New("channel: 渠道名已存在")

// CreateParams 创建渠道的参数。
type CreateParams struct {
	Name            string            // 渠道名(唯一)
	Driver          string            // rclone 后端名
	Config          map[string]string // 后端参数(含约定键 path / public_base_url),可为 nil
	Priority        int64             // 优先级,越大越优先
	Encrypted       bool              // 是否为加密渠道;创建时固化,建后不可变
	AllowDirectLink bool              // 是否允许 302 直链
}

// nameMaxLen 渠道名长度上限。
const nameMaxLen = 64

// configKeyPattern 渠道配置键的合法字符集(与 rclone 连接字符串参数名一致)。
var configKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// validateCreateParams 校验创建参数:必填项、渠道名长度、配置键字符集、
// 加密与直链的互斥(密文无法直出)。
// @param p 创建参数
// @return 参数非法时的 ErrInvalidChannel
func validateCreateParams(p CreateParams) error {
	if p.Name == "" || p.Driver == "" {
		return fmt.Errorf("%w: name 与 driver 必填", ErrInvalidChannel)
	}
	if len(p.Name) > nameMaxLen {
		return fmt.Errorf("%w: name 超过 %d 字节", ErrInvalidChannel, nameMaxLen)
	}
	if p.Encrypted && p.AllowDirectLink {
		return fmt.Errorf("%w: 加密渠道不允许直链(密文无法直出)", ErrInvalidChannel)
	}
	for k := range p.Config {
		// 约定键不进连接字符串,豁免字符集限制。
		if k == "path" || k == "public_base_url" {
			continue
		}
		if !configKeyPattern.MatchString(k) {
			return fmt.Errorf("%w: 配置键 %q 非法(仅允许字母数字下划线)", ErrInvalidChannel, k)
		}
	}
	return nil
}

// Info 渠道的对外描述(不含任何配置与凭证)。
type Info struct {
	ID              int64  `json:"id"`                // 渠道主键
	Name            string `json:"name"`              // 渠道名
	Driver          string `json:"driver"`            // 后端名
	Priority        int64  `json:"priority"`          // 优先级
	Healthy         bool   `json:"healthy"`           // 健康状态
	Encrypted       bool   `json:"encrypted"`         // 是否加密渠道
	AllowDirectLink bool   `json:"allow_direct_link"` // 是否允许直链
}

// Create 创建渠道:先试构造驱动验证配置可用,再落库并重载。
// @param p 创建参数
// @return 新渠道 ID
// @return 参数非法、后端初始化失败或落库失败时的错误
func (m *Manager) Create(ctx context.Context, p CreateParams) (int64, error) {
	if err := validateCreateParams(p); err != nil {
		return 0, err
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
	trial := sqlc.Channel{Name: p.Name, Driver: p.Driver, ConfigSealed: sealed}
	if _, _, err := m.buildDriver(ctx, trial); err != nil {
		return 0, fmt.Errorf("%w: %w", ErrInvalidChannel, err)
	}

	channelID, err := m.cfg.Queries.CreateChannel(ctx, sqlc.CreateChannelParams{
		Name:            p.Name,
		Driver:          p.Driver,
		ConfigSealed:    sealed,
		Priority:        p.Priority,
		Encrypted:       toInt(p.Encrypted),
		AllowDirectLink: toInt(p.AllowDirectLink),
	})
	if err != nil {
		// SQLite 驱动不暴露类型化的约束错误,按消息收敛为哨兵——匹配集中此一处,不外泄到 HTTP 层。
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return 0, ErrChannelNameExists
		}
		return 0, fmt.Errorf("写入渠道记录: %w", err)
	}
	// 渠道已落库;装载失败只告警(客户端已获 ID,重启或下次 Reload 可恢复)。
	if err := m.Reload(ctx); err != nil {
		m.cfg.Logger.Error("渠道已创建但装载失败", "channel", p.Name, "err", err)
	}
	return channelID, nil
}

// Delete 删除渠道;若其持有任何图的唯一就绪副本则拒绝(防数据丢失)。
// @Description: 唯一副本检查与删除在同一事务内完成,消除并发上传落库
// 造成的检查-删除窗口(TOCTOU)。
// @param channelID 渠道主键
// @return ErrHoldsSoleReplicas 或删除/重载失败时的错误
func (m *Manager) Delete(ctx context.Context, channelID int64) error {
	tx, err := m.cfg.Conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务: %w", err)
	}
	// 提交成功后 Rollback 返回 ErrTxDone,属预期,显式忽略。
	defer func() { _ = tx.Rollback() }()

	q := m.cfg.Queries.WithTx(tx)
	sole, err := q.CountSoleReplicas(ctx, channelID)
	if err != nil {
		return fmt.Errorf("检查唯一副本: %w", err)
	}
	if sole > 0 {
		return fmt.Errorf("%w(%d 张图仅此渠道有副本)", ErrHoldsSoleReplicas, sole)
	}
	// 先清副本记录(外键约束);渠道上可能残留的对象文件由对账任务回收。
	if err := q.DeleteBlobChannelsByChannel(ctx, channelID); err != nil {
		return fmt.Errorf("清理渠道副本记录: %w", err)
	}
	if err := q.DeleteChannel(ctx, channelID); err != nil {
		return fmt.Errorf("删除渠道记录: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务: %w", err)
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

// probeTimeout 单渠道健康探测超时;各渠道独立计时,慢渠道不拖累其他渠道。
const probeTimeout = 10 * time.Second

// HealthProbeKey 健康探测使用的约定对象键(从不真正写入;对账 GC 应豁免)。
const HealthProbeKey = ".imgyoung-health-probe"

// CheckAll 对全部渠道做一次健康探测,状态变化写回 DB 并更新内存标记。
// @Description: 探测 = Stat 一个约定不存在的对象:ErrNotFound 与成功都视为
// 健康(后端可达),其余错误视为不健康。
// 健康翻转只改标记、不重建驱动;单渠道异常(含记录已被删)跳过不中断整轮。
// @return 本次健康状态翻转的渠道名列表
// @return 探测本身无法进行时的错误
func (m *Manager) CheckAll(ctx context.Context) ([]string, error) {
	changed := []string{}
	for _, c := range m.All() {
		healthy := probe(ctx, c.Driver)
		row, err := m.cfg.Queries.GetChannel(ctx, c.ID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
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
		if err := m.cfg.Queries.UpdateChannelHealth(ctx, sqlc.UpdateChannelHealthParams{
			Healthy: state, ID: c.ID,
		}); err != nil {
			return changed, fmt.Errorf("更新渠道 %q 健康状态: %w", c.Name, err)
		}
		m.setHealth(c.ID, healthy)
		changed = append(changed, c.Name)
	}
	return changed, nil
}

// probe 探测单个渠道可达性(独立超时)。
// @param d 渠道驱动
// @return 是否健康
func probe(ctx context.Context, d driver.Driver) bool {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	_, err := d.Stat(probeCtx, HealthProbeKey)
	return err == nil || errors.Is(err, driver.ErrNotFound)
}
