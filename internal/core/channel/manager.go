package channel

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	"github.com/x-haose/imgyoung/internal/infra/db"
	"github.com/x-haose/imgyoung/internal/infra/secret"
	"github.com/x-haose/imgyoung/internal/store/driver"
	"github.com/x-haose/imgyoung/internal/store/rclone"
)

// ErrNoHealthyChannel 当前没有任何健康渠道可用。
var ErrNoHealthyChannel = errors.New("channel: 无健康渠道可用")

// ErrHoldsSoleReplicas 渠道持有某些图的唯一就绪副本,删除会造成数据丢失。
var ErrHoldsSoleReplicas = errors.New("channel: 渠道持有唯一副本,拒绝删除")

// Handle 渠道句柄:选择结果的最小载体。
type Handle struct {
	// ID 渠道内部主键。
	ID int64
	// Name 渠道名。
	Name string
	// Driver 就绪的存储驱动(加密渠道已含 crypt 包裹)。
	Driver driver.Driver
	// Encrypted 是否为加密渠道(密文不可直链)。
	Encrypted bool
	// AllowDirectLink 是否允许 302 直链。
	AllowDirectLink bool
	// PublicBaseURL 对外直链前缀(空 = 无直链能力);来自渠道配置键 public_base_url。
	PublicBaseURL string
}

// ManagerConfig 管理器依赖。
type ManagerConfig struct {
	// Conn 数据库连接(删除渠道等需要事务的操作用)。
	Conn *sql.DB
	// Queries sqlc 查询集。
	Queries *db.Queries
	// Master 主密钥(解密渠道配置、派生 crypt 密码)。
	Master *secret.MasterKey
	// DataDir 数据目录(内置 local 渠道的默认存储位置)。
	DataDir string
	// Logger 结构化日志。
	Logger *slog.Logger
}

// managed 一条已加载渠道的运行态。
type managed struct {
	// handle 对外句柄。
	handle Handle
	// healthy 健康标记(加载时从 DB 读取)。
	healthy bool
}

// Manager 渠道管理器:加载配置、构造驱动、优先级选择(设计定稿·渠道管理)。
// @Description: 自持驱动实例(fs.NewFs 无全局缓存),配置变更调 Reload
// 全量重建,不存在陈旧缓存窗口。读方法并发安全。
type Manager struct {
	// cfg 依赖。
	cfg ManagerConfig
	// mu 保护 channels。
	mu sync.RWMutex
	// channels 已加载渠道,按优先级降序。
	channels []managed
}

// NewManager 创建管理器并完成首次加载。
// @param cfg 依赖,字段均必填
// @return 就绪的管理器
// @return 加载或驱动构造失败时的错误
func NewManager(ctx context.Context, cfg ManagerConfig) (*Manager, error) {
	m := &Manager{cfg: cfg}
	if err := m.Reload(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

// Reload 重新加载全部渠道并重建驱动实例。
// @return 查询、解密或驱动构造失败时的错误(失败时保留旧状态)
func (m *Manager) Reload(ctx context.Context) error {
	rows, err := m.cfg.Queries.ListChannels(ctx)
	if err != nil {
		return fmt.Errorf("加载渠道列表: %w", err)
	}
	// 单渠道构造失败只跳过并告警,不拖垮其余渠道(一个坏渠道不能 brick 服务);
	// 全部失败才视为致命。
	loaded := make([]managed, 0, len(rows))
	for _, row := range rows {
		driver, params, derr := m.buildDriver(ctx, row)
		if derr != nil {
			m.cfg.Logger.Error("渠道驱动构造失败,本渠道跳过", "channel", row.Name, "err", derr)
			continue
		}
		loaded = append(loaded, managed{
			handle: Handle{
				ID:              row.ID,
				Name:            row.Name,
				Driver:          driver,
				Encrypted:       row.Encrypted != 0,
				AllowDirectLink: row.AllowDirectLink != 0,
				PublicBaseURL:   strings.TrimRight(params["public_base_url"], "/"),
			},
			healthy: row.Healthy != 0,
		})
	}
	if len(rows) > 0 && len(loaded) == 0 {
		return fmt.Errorf("全部 %d 个渠道均无法装载", len(rows))
	}
	m.mu.Lock()
	m.channels = loaded
	m.mu.Unlock()
	return nil
}

// Healthy 返回当前健康的渠道句柄,按优先级降序。
// @return 健康渠道切片(副本)
func (m *Manager) Healthy() []Handle {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Handle, 0, len(m.channels))
	for _, c := range m.channels {
		if c.healthy {
			out = append(out, c.handle)
		}
	}
	return out
}

// setHealth 更新单个渠道的内存健康标记(健康翻转无需重建驱动)。
func (m *Manager) setHealth(channelID int64, healthy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.channels {
		if m.channels[i].handle.ID == channelID {
			m.channels[i].healthy = healthy
			return
		}
	}
}

// Primary 返回主渠道:健康渠道中优先级最高者。
// @return 主渠道句柄;无健康渠道时返回 ErrNoHealthyChannel
func (m *Manager) Primary() (Handle, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, c := range m.channels {
		if c.healthy {
			return c.handle, nil
		}
	}
	return Handle{}, ErrNoHealthyChannel
}

// All 返回全部渠道句柄(含不健康者),按优先级降序。
// @return 渠道句柄切片(副本,调用方可自由持有)
func (m *Manager) All() []Handle {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Handle, len(m.channels))
	for i, c := range m.channels {
		out[i] = c.handle
	}
	return out
}

// CandidatesForBlob 返回可服务该 blob 的渠道:健康且副本已完成,按优先级降序。
// @param blobID 物理图主键
// @return 有序候选;可能为空(副本尚未就绪或渠道全部不健康)
func (m *Manager) CandidatesForBlob(ctx context.Context, blobID int64) ([]Handle, error) {
	rows, err := m.cfg.Queries.ListBlobChannels(ctx, blobID)
	if err != nil {
		return nil, fmt.Errorf("查询 blob %d 副本: %w", blobID, err)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	byID := make(map[int64]managed, len(m.channels))
	for _, c := range m.channels {
		byID[c.handle.ID] = c
	}
	out := make([]Handle, 0, len(rows))
	for _, row := range rows { // 查询已按优先级降序
		c, ok := byID[row.ChannelID]
		if ok && c.healthy && row.State == StateDone {
			out = append(out, c.handle)
		}
	}
	return out, nil
}

// SealConfig 用主密钥加密渠道参数 JSON(供渠道创建方使用)。
// @param plain 参数 JSON 明文
// @return 可入库的密文
// @return 加密失败时的错误
func (m *Manager) SealConfig(plain string) (string, error) {
	return m.cfg.Master.EncryptString(plain)
}

// buildDriver 按渠道行构造存储驱动:解密参数 → 连接字符串 → 可选 crypt 包裹。
// @param row 渠道记录
// @return 就绪的驱动与解密后的原始参数表(含约定键)
// @return 解密、解析或后端初始化失败时的错误
func (m *Manager) buildDriver(ctx context.Context, row db.Channel) (driver.Driver, map[string]string, error) {
	params := map[string]string{}
	if row.ConfigSealed != "" {
		plain, err := m.cfg.Master.DecryptString(row.ConfigSealed)
		if err != nil {
			return nil, nil, fmt.Errorf("解密渠道配置: %w", err)
		}
		if err := json.Unmarshal([]byte(plain), &params); err != nil {
			return nil, nil, fmt.Errorf("解析渠道配置 JSON: %w", err)
		}
	}
	raw := map[string]string{}
	for k, v := range params {
		raw[k] = v
	}
	// 约定键:path 为后端内路径,public_base_url 为直链前缀(均非 rclone 参数);
	// 其余为后端参数。内置 local 缺省落 dataDir/blobs。
	path := params["path"]
	delete(params, "path")
	delete(params, "public_base_url")
	if row.Driver == "local" && path == "" {
		path = filepath.Join(m.cfg.DataDir, "blobs")
	}
	applyDriverDefaults(row.Driver, params)

	remote := BuildRemote(row.Driver, params, path)
	if row.Encrypted != 0 {
		password, err := m.deriveHex(row, "crypt:password")
		if err != nil {
			return nil, nil, err
		}
		salt, err := m.deriveHex(row, "crypt:salt")
		if err != nil {
			return nil, nil, err
		}
		if remote, err = WrapCrypt(remote, password, salt); err != nil {
			return nil, nil, err
		}
	}
	driver, err := rclone.New(ctx, remote)
	if err != nil {
		return nil, nil, err
	}
	return driver, raw, nil
}

// Get 按 ID 返回渠道句柄。
// @param channelID 渠道主键
// @return 句柄与是否存在
func (m *Manager) Get(channelID int64) (Handle, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, c := range m.channels {
		if c.handle.ID == channelID {
			return c.handle, true
		}
	}
	return Handle{}, false
}

// deriveHex 为渠道派生一个 hex 编码的子密钥(crypt 密码/盐)。
// @Description: 用途标识含创建时间——SQLite 的 rowid 可能在删除后被复用,
// 仅绑 ID 会让新渠道派生出与已删渠道相同的密钥。
// @param row 渠道记录
// @param usage 用途后缀
// @return 64 位 hex 字符串
// @return 派生失败时的错误
func (m *Manager) deriveHex(row db.Channel, usage string) (string, error) {
	purpose := fmt.Sprintf("channel:%d:%s:%s", row.ID, row.CreatedAt, usage)
	raw, err := m.cfg.Master.Derive(purpose, 32)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
