// Package channel 实现渠道管理:配置构造、优先级选择、健康状态与副本编排。
package channel

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"path/filepath"
	"strings"
	"sync"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/secret"
	"github.com/x-haose/imgyoung/internal/store/driver"
	"github.com/x-haose/imgyoung/internal/store/rclone"
)

// ErrNoHealthyChannel 当前没有任何健康渠道可用。
var ErrNoHealthyChannel = errors.New("channel: 无健康渠道可用")

// ErrHoldsSoleReplicas 渠道持有某些图的唯一就绪副本,删除会造成数据丢失。
var ErrHoldsSoleReplicas = errors.New("channel: 渠道持有唯一副本,拒绝删除")

// 渠道配置中的约定键:不是 rclone 参数,由本包消费后不再下传后端。
const (
	configKeyPath          = "path"            // 后端内路径(桶名或目录)
	configKeyPublicBaseURL = "public_base_url" // 302 直链前缀
)

// Handle 渠道句柄:选择结果的最小载体。
type Handle struct {
	ID              int64         // 渠道内部主键
	Name            string        // 渠道名
	Driver          driver.Driver // 就绪的存储驱动(加密渠道已含 crypt 包裹)
	Encrypted       bool          // 是否为加密渠道(密文不可直链)
	AllowDirectLink bool          // 是否允许 302 直链
	PublicBaseURL   string        // 对外直链前缀(空 = 无直链能力);来自渠道配置键 public_base_url
}

// ManagerConfig 管理器依赖。
type ManagerConfig struct {
	Conn    *sql.DB           // 数据库写句柄(删除渠道等需要事务的操作用)
	Queries *sqlc.Queries     // sqlc 查询集
	Master  *secret.MasterKey // 主密钥(解密渠道配置、派生 crypt 密码)
	DataDir string            // 数据目录(内置 local 渠道的默认存储位置)
	Logger  *slog.Logger      // 结构化日志
}

// managed 一条已加载渠道的运行态。
type managed struct {
	handle  Handle         // 对外句柄
	healthy bool           // 健康标记(加载时从 DB 读取)
	driver  *rclone.Driver // 驱动实例,退役时 Close
	source  driverSource   // 构造驱动所用的输入,Reload 据此判定是否需要重建
}

// driverSource 决定驱动实例形态的全部输入;任一项变化即须重建驱动。
type driverSource struct {
	driver    string            // rclone 后端名
	encrypted bool              // 是否套 crypt
	createdAt string            // 渠道创建时刻(参与 crypt 密钥派生)
	params    map[string]string // 解密后的完整配置(含约定键);发布后只读,写回时整体替换
}

// equal 判定两份输入是否产出同一形态的驱动。
// @param other 另一份输入
// @return 相同为 true
func (s driverSource) equal(other driverSource) bool {
	return s.driver == other.driver && s.encrypted == other.encrypted &&
		s.createdAt == other.createdAt && maps.Equal(s.params, other.params)
}

// Manager 渠道管理器:加载配置、构造驱动、优先级选择。
// @Description: 自持驱动实例;配置变更调 Reload,只重建配置有变的渠道、关闭退役实例。
// 读方法并发安全。
type Manager struct {
	cfg      ManagerConfig // 依赖
	mu       sync.RWMutex  // 保护 channels
	channels []managed     // 已加载渠道,按优先级降序
	reloadMu sync.Mutex    // 串行化 Reload 与 Close:两者都要在"读取现状 → 构造/关闭 → 换入"之间保持现状不变
	cfgMu    sync.Mutex    // 串行化渠道配置密文的读改写(编辑与后端写回并发时不互相覆盖)
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

// Reload 按渠道表现状重建管理器状态。
// @Description: 配置未变的渠道沿用既有驱动实例(在途操作不受影响);配置有变或新增的渠道
// 构造新实例,被替换与已删除渠道的旧实例在换入后关闭。单渠道构造失败只跳过并告警,
// 不拖垮其余渠道(一个坏渠道不能 brick 服务);全部失败才视为致命,此时保留旧状态。
// @return 查询、解密或驱动构造失败时的错误
func (m *Manager) Reload(ctx context.Context) error {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()

	rows, err := m.cfg.Queries.ListChannels(ctx)
	if err != nil {
		return fmt.Errorf("加载渠道列表: %w", err)
	}
	previous := m.byID()
	loaded := make([]managed, 0, len(rows))
	for _, row := range rows {
		params, err := m.unsealConfigMap(row.ConfigSealed)
		if err != nil {
			m.cfg.Logger.Error("渠道配置无法解密,本渠道跳过", "channel", row.Name, "err", err)
			continue
		}
		source := driverSource{driver: row.Driver, encrypted: row.Encrypted != 0, createdAt: row.CreatedAt, params: params}
		drv := (*rclone.Driver)(nil)
		if prev, ok := previous[row.ID]; ok && prev.source.equal(source) {
			drv, source = prev.driver, prev.source
			delete(previous, row.ID)
		} else if drv, err = m.openDriver(ctx, row, params); err != nil {
			m.cfg.Logger.Error("渠道驱动构造失败,本渠道跳过", "channel", row.Name, "err", err)
			continue
		}
		loaded = append(loaded, managed{
			handle: Handle{
				ID:              row.ID,
				Name:            row.Name,
				Driver:          drv,
				Encrypted:       row.Encrypted != 0,
				AllowDirectLink: row.AllowDirectLink != 0,
				PublicBaseURL:   strings.TrimRight(params[configKeyPublicBaseURL], "/"),
			},
			healthy: row.Healthy != 0,
			driver:  drv,
			source:  source,
		})
	}
	if len(rows) > 0 && len(loaded) == 0 {
		return fmt.Errorf("全部 %d 个渠道均无法装载", len(rows))
	}
	m.mu.Lock()
	m.channels = loaded
	m.mu.Unlock()
	for _, prev := range previous {
		if err := prev.driver.Close(ctx); err != nil {
			m.cfg.Logger.Warn("关闭退役渠道驱动失败", "channel", prev.handle.Name, "err", err)
		}
	}
	return nil
}

// byID 返回当前已加载渠道按主键的索引(副本)。
// @return 主键 → 运行态
func (m *Manager) byID() map[int64]managed {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[int64]managed, len(m.channels))
	for _, c := range m.channels {
		out[c.handle.ID] = c
	}
	return out
}

// Close 关闭全部渠道驱动,释放后端连接与后台任务;之后管理器不可再用。
// @param ctx 控制关闭的超时与取消
// @return 任一驱动关闭失败时的错误
func (m *Manager) Close(ctx context.Context) error {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()
	m.mu.Lock()
	channels := m.channels
	m.channels = nil
	m.mu.Unlock()
	return m.closeAll(ctx, channels)
}

// closeAll 关闭一组渠道的驱动,汇总失败。
// @param channels 待关闭的渠道
// @return 任一驱动关闭失败时的错误
func (m *Manager) closeAll(ctx context.Context, channels []managed) error {
	var errs []error
	for _, c := range channels {
		if err := c.driver.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("渠道 %q: %w", c.handle.Name, err))
		}
	}
	return errors.Join(errs...)
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
	byID := m.byID()
	out := make([]Handle, 0, len(rows))
	for _, row := range rows { // 查询已按优先级降序
		c, ok := byID[row.ChannelID]
		if ok && c.healthy && row.State == StateDone {
			out = append(out, c.handle)
		}
	}
	return out, nil
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

// openDriver 为一条渠道记录构造驱动:约定键拆出、crypt 密钥派生、写回落库。
// @param row 渠道记录
// @param params 解密后的完整配置(含约定键)
// @return 就绪的驱动
// @return 密钥派生或后端初始化失败时的错误
func (m *Manager) openDriver(ctx context.Context, row sqlc.Channel, params map[string]string) (*rclone.Driver, error) {
	opts := m.driverOptions(row.Driver, params)
	channelID := row.ID
	opts.OnChange = func(key, value string) { m.persistParam(channelID, key, value) }
	if row.Encrypted != 0 {
		password, err := m.deriveHex(row, "crypt:password")
		if err != nil {
			return nil, err
		}
		salt, err := m.deriveHex(row, "crypt:salt")
		if err != nil {
			return nil, err
		}
		opts.Crypt = &rclone.CryptOptions{Password: password, Salt: salt}
	}
	return rclone.Open(ctx, opts)
}

// openTrial 用一份未落库的配置构造一次性驱动(试构造、测试连接、建桶),不套 crypt、不落写回。
// @Description: 加密属性不参与——crypt 密钥派生依赖渠道 ID 与创建时刻,试构造验证的是
// 后端连通性与参数合法性。调用方用毕须 Close。
// @param driverName rclone 后端名
// @param params 明文配置(含约定键)
// @return 就绪的驱动
// @return 后端初始化失败时的错误
func (m *Manager) openTrial(ctx context.Context, driverName string, params map[string]string) (*rclone.Driver, error) {
	return rclone.Open(ctx, m.driverOptions(driverName, params))
}

// driverOptions 把渠道配置翻译成驱动构造参数:约定键拆出,内置 local 缺省落 dataDir/blobs。
// @param driverName rclone 后端名
// @param params 明文配置(含约定键)
// @return 驱动构造参数(未设 Crypt 与 OnChange)
func (m *Manager) driverOptions(driverName string, params map[string]string) rclone.Options {
	backendParams := make(map[string]string, len(params))
	for k, v := range params {
		if k != configKeyPath && k != configKeyPublicBaseURL {
			backendParams[k] = v
		}
	}
	path := params[configKeyPath]
	if driverName == "local" && path == "" {
		path = filepath.Join(m.cfg.DataDir, "blobs")
	}
	return rclone.Options{Backend: driverName, Params: backendParams, Path: path}
}

// closeTrial 关闭一次性驱动;失败只告警——它不影响已得到的结论。
// @param d 待关闭的驱动
// @param what 用途,日志定位用
func (m *Manager) closeTrial(ctx context.Context, d *rclone.Driver, what string) {
	if err := d.Close(ctx); err != nil {
		m.cfg.Logger.Warn("关闭临时驱动失败", "what", what, "err", err)
	}
}

// persistParam 把后端运行期写回的参数(刷新后的 OAuth 令牌、onedrive 的 drive_id 等)并入渠道配置并落库。
// @Description: 写回来自 rclone 内部,不带上下文,也不该被某个请求的取消牵连,故用独立上下文。
// 落库失败只能记录——rclone 的写回接口无处接收错误;内存里的新值仍在用,重启后退回旧值。
// 同步更新本渠道的驱动输入,下次 Reload 不会把写回误判为配置变更而重建驱动。
// @param channelID 渠道主键
// @param key 参数名
// @param value 新值
func (m *Manager) persistParam(channelID int64, key, value string) {
	ctx := context.Background()
	m.cfgMu.Lock()
	defer m.cfgMu.Unlock()
	row, err := m.cfg.Queries.GetChannel(ctx, channelID)
	if err != nil {
		m.cfg.Logger.Error("后端写回参数无法落库:读取渠道失败", "channel_id", channelID, "key", key, "err", err)
		return
	}
	params, err := m.unsealConfigMap(row.ConfigSealed)
	if err != nil {
		m.cfg.Logger.Error("后端写回参数无法落库", "channel", row.Name, "key", key, "err", err)
		return
	}
	if params[key] == value {
		return
	}
	params[key] = value
	sealed, err := m.sealConfigMap(params)
	if err != nil {
		m.cfg.Logger.Error("后端写回参数无法落库", "channel", row.Name, "key", key, "err", err)
		return
	}
	if err := m.cfg.Queries.UpdateChannelConfigSealed(ctx, sqlc.UpdateChannelConfigSealedParams{
		ConfigSealed: sealed, ID: channelID,
	}); err != nil {
		m.cfg.Logger.Error("后端写回参数无法落库", "channel", row.Name, "key", key, "err", err)
		return
	}
	m.mu.Lock()
	for i := range m.channels {
		if m.channels[i].handle.ID == channelID {
			next := maps.Clone(m.channels[i].source.params)
			next[key] = value
			m.channels[i].source.params = next
		}
	}
	m.mu.Unlock()
	m.cfg.Logger.Info("渠道参数已由后端写回并落库", "channel", row.Name, "key", key)
}

// sealConfigMap 把渠道配置编码为 JSON 并用主密钥加密成可入库密文。
// @param config 明文配置,可为 nil
// @return 可入库的密文
// @return 编码或加密失败时的错误
func (m *Manager) sealConfigMap(config map[string]string) (string, error) {
	cfgJSON, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("编码渠道配置: %w", err)
	}
	sealed, err := m.cfg.Master.EncryptString(string(cfgJSON))
	if err != nil {
		return "", fmt.Errorf("加密渠道配置: %w", err)
	}
	return sealed, nil
}

// unsealConfigMap 把入库的配置密文解密还原为参数表。
// @param sealed 配置密文,空串表示无参数
// @return 参数表(含约定键 path / public_base_url),永不为 nil
// @return 解密或解析失败时的错误
func (m *Manager) unsealConfigMap(sealed string) (map[string]string, error) {
	params := map[string]string{}
	if sealed == "" {
		return params, nil
	}
	plain, err := m.cfg.Master.DecryptString(sealed)
	if err != nil {
		return nil, fmt.Errorf("解密渠道配置: %w", err)
	}
	if err := json.Unmarshal([]byte(plain), &params); err != nil {
		return nil, fmt.Errorf("解析渠道配置 JSON: %w", err)
	}
	return params, nil
}

// deriveHex 为渠道派生一个 hex 编码的子密钥(crypt 密码/盐)。
// @Description: 用途标识含创建时间——SQLite 的 rowid 可能在删除后被复用,
// 仅绑 ID 会让新渠道派生出与已删渠道相同的密钥。
// @param row 渠道记录
// @param usage 用途后缀
// @return 64 位 hex 字符串
// @return 派生失败时的错误
func (m *Manager) deriveHex(row sqlc.Channel, usage string) (string, error) {
	purpose := fmt.Sprintf("channel:%d:%s:%s", row.ID, row.CreatedAt, usage)
	raw, err := m.cfg.Master.Derive(purpose, 32)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
