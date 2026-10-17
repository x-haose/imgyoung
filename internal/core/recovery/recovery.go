// Package recovery 按渠道里的对象找回图片记录:元数据库退回到较早的状态后(从快照恢复,或最近的写入还没来得及
// 复制出去),之后上传的图片对象仍在渠道里,库里却没有记录,链接返回 404。
// 本包列举渠道、挑出键名合规而库里没有记录的对象,读取并校验内容后按对象键里的短 ID 重新登记。
package recovery

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/upload"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/id"
	"github.com/x-haose/imgyoung/store/driver"
)

// ErrBusy 已有一轮找回在进行。
var ErrBusy = errors.New("recovery: 已有找回在进行")

// ErrUserNotFound 指定的归属用户不存在。
var ErrUserNotFound = errors.New("recovery: 归属用户不存在")

// ErrIDTaken 对象键里的短 ID 已属于指向另一个对象的图片记录。
var ErrIDTaken = errors.New("recovery: 对象键里的短 ID 已属于另一条图片记录")

// objectTimeout 找回单个对象(读取、校验、登记)的时限。
const objectTimeout = 5 * time.Minute

// maxFailures 结果里逐条列出的失败对象数上限;超出的只计数。
const maxFailures = 100

// 失败原因,取值即接口契约的枚举。
const (
	ReasonReadFailed     = "read_failed"     // 持有该对象的渠道都读取失败;渠道恢复后重试
	ReasonTooLarge       = "too_large"       // 对象超过当前的单图字节上限
	ReasonInvalidImage   = "invalid_image"   // 内容不是可解析的白名单格式,或超出像素预算
	ReasonKeyMismatch    = "key_mismatch"    // 内容的哈希与对象键里的哈希前缀不符
	ReasonIDTaken        = "id_taken"        // 对象键里的短 ID 已属于另一条图片记录
	ReasonRegisterFailed = "register_failed" // 登记时落库失败
)

// Failure 一个没有找回的对象。
type Failure struct {
	Key    string `json:"key"`    // 渠道内对象键
	Reason string `json:"reason"` // 失败原因,Reason* 常量之一
	Detail string `json:"detail"` // 根因原文,供排障
}

// Result 一轮找回的统计。
type Result struct {
	Scanned   int       `json:"scanned"`   // 健康渠道上键名合规的对象数,跨渠道按对象键去重
	Recorded  int       `json:"recorded"`  // 其中库里已有记录的对象数,含正在上传、即将落库的
	Recovered int       `json:"recovered"` // 重新登记的图片数
	Failed    int       `json:"failed"`    // 没有找回的对象数
	Failures  []Failure `json:"failures"`  // 没有找回的对象,至多 maxFailures 条,按对象键升序
}

// Config 找回服务依赖。
type Config struct {
	Queries  *sqlc.Queries          // 查询集
	Channels *channel.Manager       // 渠道管理器:在役渠道的列举与读取
	Upload   *upload.Service        // 登记图片
	Settings func() config.Settings // 运营配置快照读取口,取单图字节上限
	Logger   *slog.Logger           // 结构化日志
}

// Service 找回服务。
type Service struct {
	cfg Config
	mu  sync.Mutex // 同一时刻只允许一轮找回
}

// New 创建找回服务。
// @param cfg 依赖,字段均必填
// @return 就绪的找回服务
func New(cfg Config) *Service {
	return &Service{cfg: cfg}
}

// held 持有某个对象的渠道,按优先级降序。
type held []channel.Handle

// Recover 找回健康渠道上缺少记录的图片。
// @Description: 每个对象的登记是一个独立事务,失败的对象不影响其余对象;整轮可重复执行,已登记的对象下一轮被识别为
// 已有记录。ctx 取消即中止,已登记的保留。找回的图片永不过期,没有文件名与来源 Token。
// 同一对象存在于多个渠道时,按优先级从第一个读得出的渠道取字节。
// @param ctx 控制超时与取消
// @param userID 找回的图片归属的用户 id
// @return 统计;ctx 取消时为已完成部分
// @return ErrBusy、ErrUserNotFound;没有健康渠道时包裹 channel.ErrNoHealthyChannel;列举渠道或读取已有记录失败、
// ctx 取消时的错误
func (s *Service) Recover(ctx context.Context, userID string) (Result, error) {
	if !s.mu.TryLock() {
		return Result{}, ErrBusy
	}
	defer s.mu.Unlock()
	if _, err := s.cfg.Queries.GetUser(ctx, userID); errors.Is(err, sql.ErrNoRows) {
		return Result{}, ErrUserNotFound
	} else if err != nil {
		return Result{}, fmt.Errorf("查询归属用户: %w", err)
	}
	targets := s.cfg.Channels.Healthy()
	if len(targets) == 0 {
		return Result{}, fmt.Errorf("选择渠道: %w", channel.ErrNoHealthyChannel)
	}
	found, err := survey(ctx, targets)
	if err != nil {
		return Result{}, err
	}
	known, err := s.recordedKeys(ctx)
	if err != nil {
		return Result{}, err
	}
	res := Result{Scanned: len(found), Failures: []Failure{}}
	keys := make([]string, 0, len(found))
	for key := range found {
		if _, ok := known[key]; ok {
			res.Recorded++
			continue
		}
		keys = append(keys, key)
	}
	slices.SortFunc(keys, cmp.Compare[string])
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		recorded, f := s.recoverOne(ctx, userID, key, found[key])
		switch {
		case recorded:
			res.Recorded++
		case f != nil:
			res.fail(*f)
			s.cfg.Logger.Warn("对象没有找回", "key", f.Key, "reason", f.Reason, "detail", f.Detail)
		default:
			res.Recovered++
		}
	}
	s.cfg.Logger.Info("图片记录找回完成", "scanned", res.Scanned, "recorded", res.Recorded,
		"recovered", res.Recovered, "failed", res.Failed)
	return res, nil
}

// fail 记下一个没有找回的对象。
// @param f 失败项
func (r *Result) fail(f Failure) {
	r.Failed++
	if len(r.Failures) < maxFailures {
		r.Failures = append(r.Failures, f)
	}
}

// survey 列举各渠道,汇总键名合规的对象。
// @param ctx 控制超时与取消
// @param targets 要列举的渠道,按优先级降序
// @return 对象键 → 持有它的渠道
// @return 任一渠道列举失败时的错误
func survey(ctx context.Context, targets []channel.Handle) (map[string]held, error) {
	found := map[string]held{}
	for _, h := range targets {
		objs, err := h.Driver.List(ctx, "")
		if err != nil {
			return nil, fmt.Errorf("列举渠道 %q: %w", h.Name, err)
		}
		for _, o := range objs {
			if !id.IsStorageKey(o.Key) {
				continue
			}
			found[o.Key] = append(found[o.Key], h)
		}
	}
	return found, nil
}

// recordedKeys 读出库里全部物理图的对象键,含待删除的:它们的对象还在渠道上,不能当作缺记录重新登记。
// @return 对象键集合
// @return 查询失败时的错误
func (s *Service) recordedKeys(ctx context.Context) (map[string]struct{}, error) {
	keys, err := s.cfg.Queries.ListBlobStorageKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询已有对象键: %w", err)
	}
	set := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		set[k] = struct{}{}
	}
	return set, nil
}

// recoverOne 找回一个对象。
// @Description: 正在上传的对象、以及在本轮列举之后才落库的对象已经或即将有记录,按已有记录处理。
// @param userID 归属用户 id
// @param key 对象键
// @param holders 持有该对象的渠道
// @return 对象已有记录(含正在上传)时为 true
// @return 没有找回时的失败项;找回成功或已有记录时为 nil
func (s *Service) recoverOne(ctx context.Context, userID, key string, holders held) (bool, *Failure) {
	fail := func(reason string, err error) (bool, *Failure) {
		return false, &Failure{Key: key, Reason: reason, Detail: err.Error()}
	}
	if s.cfg.Upload.Uploading(key) {
		return true, nil
	}
	recorded, err := s.hasRecord(ctx, key)
	switch {
	case errors.Is(err, ErrIDTaken):
		return fail(ReasonIDTaken, err)
	case err != nil:
		return fail(ReasonRegisterFailed, err)
	case recorded:
		return true, nil
	}
	ctx, cancel := context.WithTimeout(ctx, objectTimeout)
	defer cancel()
	data, err := readFirst(ctx, holders, key, s.cfg.Settings().MaxUploadBytes())
	switch {
	case errors.Is(err, errTooLarge):
		return fail(ReasonTooLarge, err)
	case err != nil:
		return fail(ReasonReadFailed, err)
	}
	ids := make([]int64, len(holders))
	for i, h := range holders {
		ids[i] = h.ID
	}
	_, err = s.cfg.Upload.Adopt(ctx, upload.AdoptParams{Key: key, Data: data, UserID: userID, Holders: ids})
	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, upload.ErrKeyMismatch):
		return fail(ReasonKeyMismatch, err)
	case errors.Is(err, upload.ErrUnsupportedType), errors.Is(err, upload.ErrInvalidImage),
		errors.Is(err, upload.ErrTooManyPixels):
		return fail(ReasonInvalidImage, err)
	default:
		return fail(ReasonRegisterFailed, err)
	}
}

// hasRecord 判断对象键里的短 ID 在库里是否已经是这个对象的图片。
// @param key 对象键
// @return 短 ID 的图片存在且指向这个对象为 true;短 ID 不存在为 false
// @return 短 ID 已属于指向别的对象的图片(ErrIDTaken),或查询失败时的错误
func (s *Service) hasRecord(ctx context.Context, key string) (bool, error) {
	shortID := key[:id.Length]
	exists, err := s.cfg.Queries.ImageExists(ctx, shortID)
	if err != nil {
		return false, fmt.Errorf("查询短 ID: %w", err)
	}
	if exists == 0 {
		return false, nil
	}
	blobID, err := s.cfg.Queries.GetImageBlobID(ctx, shortID)
	if err != nil {
		return false, fmt.Errorf("查询短 ID 的物理图: %w", err)
	}
	blob, err := s.cfg.Queries.GetBlob(ctx, blobID)
	if err != nil {
		return false, fmt.Errorf("查询物理图: %w", err)
	}
	if blob.StorageKey != key {
		return false, ErrIDTaken
	}
	return true, nil
}

// errTooLarge 读取途中发现对象超过字节上限。
var errTooLarge = errors.New("对象超过单图字节上限")

// readFirst 按优先级从持有对象的渠道读出完整字节,读取失败的渠道换下一个。
// @param holders 持有对象的渠道,按优先级降序
// @param key 对象键
// @param limit 字节上限
// @return 对象字节
// @return 超过上限时包裹 errTooLarge;各渠道都读取失败时列出各渠道原因
func readFirst(ctx context.Context, holders held, key string, limit int64) ([]byte, error) {
	var failures []error
	for _, h := range holders {
		data, err := readAll(ctx, h.Driver, key, limit)
		if err == nil || errors.Is(err, errTooLarge) {
			return data, err
		}
		failures = append(failures, fmt.Errorf("渠道 %q: %w", h.Name, err))
	}
	return nil, errors.Join(failures...)
}

// readAll 从单个渠道读出对象的完整字节。
// @param d 渠道驱动
// @param key 对象键
// @param limit 字节上限
// @return 对象字节
// @return 打开或读取失败时的错误;超过上限时包裹 errTooLarge
func readAll(ctx context.Context, d driver.Driver, key string, limit int64) ([]byte, error) {
	rc, err := d.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errTooLarge
	}
	return data, nil
}
