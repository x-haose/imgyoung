package config

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
	"github.com/x-haose/imgyoung/internal/infra/secret"
)

// 运营配置修改的哨兵错误,调用方据此映射 HTTP 状态码。
var (
	ErrUnknownKey     = errors.New("config: 未知配置键")
	ErrInvalidSetting = errors.New("config: 配置值非法")
)

// cdnAPITokenKey 凭证类配置键:入库 AES-GCM 密文,内存明文,出口一律脱敏。
const cdnAPITokenKey = "cdn_cloudflare_api_token"

// 凭证类变更在审计里的脱敏表述。
const (
	maskedSet   = "(已配置)" // 值非空
	maskedUnset = "(空)"   // 值为空
)

// Change 一次配置修改中单个键的变更记录(审计留痕用);凭证类键的新旧值已脱敏。
type Change struct {
	Key string // 配置键
	Old string // 修改前取值(JSON 文本)
	New string // 修改后取值(JSON 文本)
}

// Store 运营配置存储:settings 单行宽表是唯一真相,内存快照经 atomic.Pointer 发布,
// 读路径无锁取当前值,修改校验后落库并原子换入,热生效不重启。
type Store struct {
	conn    *sql.DB                  // 事务入口(修改在事务内提交)
	queries *sqlc.Queries            // settings 表查询集
	master  *secret.MasterKey        // 凭证类值的加解密密钥
	mu      sync.Mutex               // 串行化写路径(落库+换快照全程),保证快照顺序与 DB 一致
	snap    atomic.Pointer[Settings] // 当前生效配置快照
	changed chan struct{}            // 变更广播:每次成功换快照即关闭并替换,mu 保护
}

// LoadStore 装载运营配置:确保默认行存在(列 DEFAULT 即出厂值),读入快照。
// @Description: 首启插入 id=1 的默认行;之后以表为准。凭证列解密回内存明文,
// 密钥不匹配属致命,快速失败。生效配置校验失败即启动失败,不以非法配置对外。
// @param ctx 控制装载超时与取消
// @param conn 数据库连接
// @param queries 查询集
// @param master 主密钥(凭证类值加解密)
// @return 就绪的存储
// @return 数据库、解密或校验错误
func LoadStore(ctx context.Context, conn *sql.DB, queries *sqlc.Queries, master *secret.MasterKey) (*Store, error) {
	if err := queries.EnsureSettingsRow(ctx); err != nil {
		return nil, fmt.Errorf("初始化配置默认行: %w", err)
	}
	s := &Store{conn: conn, queries: queries, master: master, changed: make(chan struct{})}
	cur, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	if err := cur.validate(); err != nil {
		return nil, fmt.Errorf("生效配置非法(settings 表): %w", err)
	}
	s.snap.Store(&cur)
	return s, nil
}

// Current 返回当前生效配置快照。
// @Description: 切片字段与快照共享底层数组,调用方只读、不得修改。
// @return 配置快照
func (s *Store) Current() Settings { return *s.snap.Load() }

// Changed 返回配置变更广播通道:任一次修改生效即关闭它。
// @Description: 关闭即广播,监听方据此立刻按新快照重算自身参数——周期任务不必等旧周期
// 走完才用上新间隔,兑现"保存即生效"。通道一次性,每轮监听都要重新取。
// @return 只读通道,下次变更时被关闭
func (s *Store) Changed() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.changed
}

// Update 应用一组按键的修改:合并当前快照 → 校验 → 落库 → 原子换入。
// @Description: patch 里未出现的键不动;全部键落空(取值未变)时不写库不换快照。
// 凭证键传空串表示清除、不传表示不动。未知键与非法值整体拒绝、不做部分生效。
// @param ctx 控制超时与取消
// @param patch 键 → 新值(JSON 编码);键必须是已知运营配置键
// @return 实际发生变化的键列表(凭证类值已脱敏),供审计留痕
// @return ErrUnknownKey / ErrInvalidSetting 或数据库错误
func (s *Store) Update(ctx context.Context, patch map[string]json.RawMessage) ([]Change, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.snap.Load()
	next, err := applyPatch(*cur, patch)
	if err != nil {
		return nil, err
	}
	changes, err := diffSettings(*cur, next, patch)
	if err != nil {
		return nil, err
	}
	if len(changes) == 0 {
		return nil, nil
	}
	params, err := s.toParams(next)
	if err != nil {
		return nil, err
	}
	if err := s.persist(ctx, params); err != nil {
		return nil, err
	}
	s.snap.Store(&next)
	close(s.changed)
	s.changed = make(chan struct{})
	return changes, nil
}

// applyPatch 把 patch 覆盖到当前配置上并归一、校验,返回候选新配置。
// @Description: patch 反序列化进当前配置副本——只覆盖出现的键;未知键(DisallowUnknownFields)
// 或类型不符即 ErrUnknownKey / ErrInvalidSetting,两者的错误链上都带指明配置键的错误码。
// @param cur 当前配置
// @param patch 键 → 新值
// @return 校验通过的新配置
// @return ErrUnknownKey / ErrInvalidSetting;patch 无法重新编码时的错误
func applyPatch(cur Settings, patch map[string]json.RawMessage) (Settings, error) {
	blob, err := json.Marshal(patch)
	if err != nil {
		return Settings{}, fmt.Errorf("编码配置修改: %w", err)
	}
	next := cur
	// 切片字段深拷贝后再解码:json 解码数组会复用已有底层数组,不克隆则 Decode 会在
	// 校验通过、原子换入之前就地改写当前生效快照的数组——破坏"非法值整体拒绝"的原子性,
	// 且与访问路径对 Current() 里各切片字段的无锁读形成数据竞争。
	next.ProcessWidths = append([]int(nil), cur.ProcessWidths...)
	next.ProcessFormats = append([]string(nil), cur.ProcessFormats...)
	next.SpoofCountries = append([]string{}, cur.SpoofCountries...)
	dec := json.NewDecoder(bytes.NewReader(blob))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&next); err != nil {
		return Settings{}, decodeError(err)
	}
	next = next.normalized()
	if err := next.validate(); err != nil {
		return Settings{}, fmt.Errorf("%w: %w", ErrInvalidSetting, err)
	}
	return next, nil
}

// decodeError 把 patch 解码失败翻译为带错误码的配置错误。
// @Description: encoding/json 对未知字段只给出形如 json: unknown field "x" 的文本,字段名从中切出;
// 类型不符给出 *json.UnmarshalTypeError,字段名取其 Field。
// @param err 解码错误
// @return 包裹 ErrUnknownKey 或 ErrInvalidSetting 的错误
func decodeError(err error) error {
	if _, name, ok := strings.Cut(err.Error(), "unknown field "); ok {
		return fmt.Errorf("%w: %w", ErrUnknownKey, errcode.New(errcode.SettingUnknown, "key", strings.Trim(name, `"`)))
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return fmt.Errorf("%w: %w", ErrInvalidSetting, errcode.New(errcode.SettingType, "key", typeErr.Field))
	}
	return fmt.Errorf("%w: %w", ErrInvalidSetting, err)
}

// diffSettings 对 patch 涉及的键比较新旧取值,产出实际变化清单(凭证键脱敏)。
// @param cur 修改前配置
// @param next 修改后配置
// @param patch 本次请求涉及的键
// @return 变化清单,按键名排序保证审计输出稳定
// @return 序列化错误
func diffSettings(cur, next Settings, patch map[string]json.RawMessage) ([]Change, error) {
	curMap, err := toJSONMap(cur)
	if err != nil {
		return nil, err
	}
	nextMap, err := toJSONMap(next)
	if err != nil {
		return nil, err
	}
	changes := make([]Change, 0, len(patch))
	for key := range patch {
		if string(curMap[key]) == string(nextMap[key]) {
			continue
		}
		changes = append(changes, Change{
			Key: key,
			Old: maskValue(key, curMap[key]),
			New: maskValue(key, nextMap[key]),
		})
	}
	slices.SortFunc(changes, func(a, b Change) int { return strings.Compare(a.Key, b.Key) })
	return changes, nil
}

// persist 把新配置在事务内写入 settings 表(单行 UPDATE)。
// @param ctx 控制超时与取消
// @param params 已编码的列参数(凭证列为密文)
// @return 数据库错误
func (s *Store) persist(ctx context.Context, params sqlc.UpdateSettingsParams) error {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启配置写入事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.queries.WithTx(tx).UpdateSettings(ctx, params); err != nil {
		return fmt.Errorf("写入配置: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交配置写入事务: %w", err)
	}
	return nil
}

// load 读出 settings 单行并映射为内存配置:标量列直接换算,凭证列解密、档位列解析 JSON。
// @param ctx 控制超时与取消
// @return 内存配置
// @return 数据库、解密或 JSON 解析错误
func (s *Store) load(ctx context.Context) (Settings, error) {
	row, err := s.queries.GetSettings(ctx)
	if err != nil {
		return Settings{}, fmt.Errorf("读取 settings 表: %w", err)
	}
	cfg := scalarsFromRow(row)
	if row.CdnCloudflareApiToken != "" {
		if cfg.CDNCloudflareAPIToken, err = s.master.DecryptString(row.CdnCloudflareApiToken); err != nil {
			return Settings{}, fmt.Errorf("解密 cdn_cloudflare_api_token: %w", err)
		}
	}
	if err := json.Unmarshal([]byte(row.ProcessWidths), &cfg.ProcessWidths); err != nil {
		return Settings{}, fmt.Errorf("解析 process_widths: %w", err)
	}
	if err := json.Unmarshal([]byte(row.ProcessFormats), &cfg.ProcessFormats); err != nil {
		return Settings{}, fmt.Errorf("解析 process_formats: %w", err)
	}
	if err := json.Unmarshal([]byte(row.SpoofCountries), &cfg.SpoofCountries); err != nil {
		return Settings{}, fmt.Errorf("解析 spoof_countries: %w", err)
	}
	return cfg, nil
}

// scalarsFromRow 换算无需解码的标量列(int64→int、0/1→bool、明文字符串)。
// @Description: 凭证密文列与 JSON 档位列不在此映射,返回值里对应字段为零值,由调用方解码后填入。
// @param row settings 表行
// @return 仅含标量字段的内存配置
func scalarsFromRow(row sqlc.Setting) Settings {
	return Settings{
		MaxUploadMB:            row.MaxUploadMb,
		MaxPixelsMP:            row.MaxPixelsMp,
		MaxConcurrentUploads:   int(row.MaxConcurrentUploads),
		UploadRatePerMin:       int(row.UploadRatePerMin),
		BaseURL:                row.BaseUrl,
		CDNCloudflareZoneID:    row.CdnCloudflareZoneID,
		AllowEmptyReferer:      row.AllowEmptyReferer != 0,
		EXIFPolicy:             imaging.EXIFPolicy(row.ExifPolicy),
		MinSuccess:             int(row.MinSuccess),
		AccessMode:             row.AccessMode,
		EncryptNewChannels:     row.EncryptNewChannels != 0,
		HealthCheckSeconds:     int(row.HealthCheckSeconds),
		BackupIntervalHours:    int(row.BackupIntervalHours),
		BackupKeep:             int(row.BackupKeep),
		CleanupSweepSeconds:    int(row.CleanupSweepSeconds),
		DeleteGraceMinutes:     int(row.DeleteGraceMinutes),
		DefaultTTLHours:        int(row.DefaultTtlHours),
		TTLRenewOnAccess:       row.TtlRenewOnAccess != 0,
		ActivityFlushSeconds:   int(row.ActivityFlushSeconds),
		ColdCleanupDays:        int(row.ColdCleanupDays),
		UsageReportSeconds:     int(row.UsageReportSeconds),
		ChannelQuotaGB:         int(row.ChannelQuotaGb),
		DedupScanSeconds:       int(row.DedupScanSeconds),
		DedupPhashMaxDistance:  int(row.DedupPhashMaxDistance),
		ProcessQualityWebP:     int(row.ProcessQualityWebp),
		ProcessQualityJPEG:     int(row.ProcessQualityJpeg),
		ProcessWebPMethod:      int(row.ProcessWebpMethod),
		ProcessWorkers:         int(row.ProcessWorkers),
		ProcessTimeoutMS:       int(row.ProcessTimeoutMs),
		VariantsCacheMB:        row.VariantsCacheMb,
		IdempotencyKeyTTLHours: int(row.IdempotencyKeyTtlHours),
		AlertWebhookURL:        row.AlertWebhookUrl,
		AuditRetentionDays:     int(row.AuditRetentionDays),
		ReplicateLimitMBps:     int(row.ReplicateLimitMbps),
		SpoofMode:              row.SpoofMode,
		SpoofCamera:            row.SpoofCamera,
		SpoofCity:              row.SpoofCity,
		SpoofWindowDays:        int(row.SpoofWindowDays),
		MaxSidePx:              int(row.MaxSidePx),
		MaxAspectRatio:         int(row.MaxAspectRatio),
	}
}

// toParams 把内存配置映射为 UPDATE 列参数:标量列直接换算,凭证列加密、档位列编码 JSON。
// @param cfg 待写入的配置
// @return 列参数
// @return 加密或 JSON 编码错误
func (s *Store) toParams(cfg Settings) (sqlc.UpdateSettingsParams, error) {
	params := scalarsToParams(cfg)
	if cfg.CDNCloudflareAPIToken != "" {
		sealed, err := s.master.EncryptString(cfg.CDNCloudflareAPIToken)
		if err != nil {
			return sqlc.UpdateSettingsParams{}, fmt.Errorf("加密 cdn_cloudflare_api_token: %w", err)
		}
		params.CdnCloudflareApiToken = sealed
	}
	widths, err := json.Marshal(cfg.ProcessWidths)
	if err != nil {
		return sqlc.UpdateSettingsParams{}, fmt.Errorf("编码 process_widths: %w", err)
	}
	formats, err := json.Marshal(cfg.ProcessFormats)
	if err != nil {
		return sqlc.UpdateSettingsParams{}, fmt.Errorf("编码 process_formats: %w", err)
	}
	countries, err := json.Marshal(cfg.SpoofCountries)
	if err != nil {
		return sqlc.UpdateSettingsParams{}, fmt.Errorf("编码 spoof_countries: %w", err)
	}
	params.SpoofCountries = string(countries)
	params.ProcessWidths = string(widths)
	params.ProcessFormats = string(formats)
	return params, nil
}

// scalarsToParams 换算无需编码的标量列(int→int64、bool→0/1、明文字符串)。
// @Description: 凭证密文列与 JSON 档位列不在此映射,返回值里对应参数为空串,由调用方编码后填入。
// @param cfg 待写入的配置
// @return 仅含标量列的 UPDATE 参数
func scalarsToParams(cfg Settings) sqlc.UpdateSettingsParams {
	return sqlc.UpdateSettingsParams{
		MaxUploadMb:            cfg.MaxUploadMB,
		MaxPixelsMp:            cfg.MaxPixelsMP,
		MaxConcurrentUploads:   int64(cfg.MaxConcurrentUploads),
		UploadRatePerMin:       int64(cfg.UploadRatePerMin),
		BaseUrl:                cfg.BaseURL,
		CdnCloudflareZoneID:    cfg.CDNCloudflareZoneID,
		AllowEmptyReferer:      b2i(cfg.AllowEmptyReferer),
		ExifPolicy:             string(cfg.EXIFPolicy),
		MinSuccess:             int64(cfg.MinSuccess),
		AccessMode:             cfg.AccessMode,
		EncryptNewChannels:     b2i(cfg.EncryptNewChannels),
		HealthCheckSeconds:     int64(cfg.HealthCheckSeconds),
		BackupIntervalHours:    int64(cfg.BackupIntervalHours),
		BackupKeep:             int64(cfg.BackupKeep),
		CleanupSweepSeconds:    int64(cfg.CleanupSweepSeconds),
		DeleteGraceMinutes:     int64(cfg.DeleteGraceMinutes),
		DefaultTtlHours:        int64(cfg.DefaultTTLHours),
		TtlRenewOnAccess:       b2i(cfg.TTLRenewOnAccess),
		ActivityFlushSeconds:   int64(cfg.ActivityFlushSeconds),
		ColdCleanupDays:        int64(cfg.ColdCleanupDays),
		UsageReportSeconds:     int64(cfg.UsageReportSeconds),
		ChannelQuotaGb:         int64(cfg.ChannelQuotaGB),
		DedupScanSeconds:       int64(cfg.DedupScanSeconds),
		DedupPhashMaxDistance:  int64(cfg.DedupPhashMaxDistance),
		ProcessQualityWebp:     int64(cfg.ProcessQualityWebP),
		ProcessQualityJpeg:     int64(cfg.ProcessQualityJPEG),
		ProcessWebpMethod:      int64(cfg.ProcessWebPMethod),
		ProcessWorkers:         int64(cfg.ProcessWorkers),
		ProcessTimeoutMs:       int64(cfg.ProcessTimeoutMS),
		VariantsCacheMb:        cfg.VariantsCacheMB,
		IdempotencyKeyTtlHours: int64(cfg.IdempotencyKeyTTLHours),
		AlertWebhookUrl:        cfg.AlertWebhookURL,
		AuditRetentionDays:     int64(cfg.AuditRetentionDays),
		ReplicateLimitMbps:     int64(cfg.ReplicateLimitMBps),
		SpoofMode:              cfg.SpoofMode,
		SpoofCamera:            cfg.SpoofCamera,
		SpoofCity:              cfg.SpoofCity,
		SpoofWindowDays:        int64(cfg.SpoofWindowDays),
		MaxSidePx:              int64(cfg.MaxSidePx),
		MaxAspectRatio:         int64(cfg.MaxAspectRatio),
	}
}

// b2i 布尔转 SQLite 的 0/1 整数。
// @param b 布尔值
// @return 1 表示 true,0 表示 false
func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// maskValue 审计输出前对凭证类键脱敏,其余原样返回 JSON 文本。
// @param key 配置键
// @param value JSON 值
// @return 可入审计的文本
func maskValue(key string, value json.RawMessage) string {
	if key != cdnAPITokenKey {
		return string(value)
	}
	if string(value) == `""` {
		return maskedUnset
	}
	return maskedSet
}

// toJSONMap 把 Settings 序列化为 键 → JSON 值 的映射(json 标签即键名)。
// @param cfg 配置
// @return 键值映射
// @return 序列化错误(正常不会发生)
func toJSONMap(cfg Settings) (map[string]json.RawMessage, error) {
	blob, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("序列化配置: %w", err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(blob, &out); err != nil {
		return nil, fmt.Errorf("展开配置键值: %w", err)
	}
	return out, nil
}
