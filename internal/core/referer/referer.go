// Package referer 管理防盗链白名单:规则真相在 referer_whitelist 表,
// 内存快照供访问路径无锁读取,后台增删即时刷新(热生效,无需重启)。
package referer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"

	sqlite "modernc.org/sqlite"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// 白名单管理哨兵错误,调用方据此映射 HTTP 状态码。
var (
	ErrPatternInvalid  = errors.New("referer: 规则格式非法,应为裸域(example.com)或子域通配(*.example.com)")
	ErrPatternExists   = errors.New("referer: 规则已存在")
	ErrPatternNotFound = errors.New("referer: 规则不存在")
)

// maxPatternLen 单条规则长度上限,与域名总长上限一致。
const maxPatternLen = 253

// patternRe 规则语法:可选 "*." 前缀 + 一至多段字母数字(段内可含连字符)的主机名。
// 单段主机(如 localhost、内网主机名)合法;scheme/路径/端口/空白均不合法。
var patternRe = regexp.MustCompile(`^(\*\.)?[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

// sqliteConstraintUnique modernc 驱动的唯一约束冲突扩展错误码(SQLITE_CONSTRAINT_UNIQUE)。
const sqliteConstraintUnique = 2067

// NormalizePattern 归一并校验一条规则:去首尾空白、转小写,语法非法返回错误。
// @param raw 用户输入的原始规则
// @return 归一后的规则(落库与匹配形态)
// @return ErrPatternInvalid
func NormalizePattern(raw string) (string, error) {
	p := strings.ToLower(strings.TrimSpace(raw))
	if p == "" || len(p) > maxPatternLen || !patternRe.MatchString(p) {
		return "", ErrPatternInvalid
	}
	return p, nil
}

// Service 防盗链白名单服务;快照经 atomic.Pointer 发布,读多写少下并发安全。
type Service struct {
	queries  *sqlc.Queries            // 规则表查询集
	mu       sync.Mutex               // 串行化写路径(写库+refresh 全程),保证快照发布顺序与 DB 变更顺序一致
	snapshot atomic.Pointer[[]string] // 当前生效规则快照,访问路径只读它
}

// New 创建服务:表为空时以 seeds 一次性播种(配置文件白名单的首启迁移口),
// 随后装载快照。
// @Description: 播种幂等——表非空(含手动清空前已有规则)则 seeds 整体忽略,
// 规则真相在表;手动清空全部规则并重启会按配置重新播种,不想要种子须同时清配置。
// 任一 seed 语法非法即报错,快速失败优于静默丢规则。
// @param ctx 控制装载超时与取消
// @param queries 查询集
// @param seeds 首启种子规则,可空
// @return 就绪的服务
// @return 种子非法或数据库错误
func New(ctx context.Context, queries *sqlc.Queries, seeds []string) (*Service, error) {
	s := &Service{queries: queries}
	if err := s.seed(ctx, seeds); err != nil {
		return nil, err
	}
	if err := s.refresh(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// Patterns 返回当前生效规则快照;空切片表示未启用防盗链。
// @return 规则列表(只读,调用方不得修改)
func (s *Service) Patterns() []string {
	return *s.snapshot.Load()
}

// List 返回全部规则行(后台列表用,含 id 与创建时刻)。
// @param ctx 控制超时与取消
// @return 按添加顺序排列的规则行
// @return 数据库错误
func (s *Service) List(ctx context.Context) ([]sqlc.RefererWhitelist, error) {
	rows, err := s.queries.ListRefererWhitelist(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询防盗链规则: %w", err)
	}
	return rows, nil
}

// Add 校验并新增一条规则,成功后即时刷新快照。
// @param ctx 控制超时与取消
// @param raw 用户输入的原始规则
// @return 新增的规则行
// @return ErrPatternInvalid / ErrPatternExists 或数据库错误
func (s *Service) Add(ctx context.Context, raw string) (sqlc.RefererWhitelist, error) {
	pattern, err := NormalizePattern(raw)
	if err != nil {
		return sqlc.RefererWhitelist{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	row, err := s.queries.CreateRefererPattern(ctx, pattern)
	if err != nil {
		var se *sqlite.Error
		if errors.As(err, &se) && se.Code() == sqliteConstraintUnique {
			return sqlc.RefererWhitelist{}, ErrPatternExists
		}
		return sqlc.RefererWhitelist{}, fmt.Errorf("写入防盗链规则: %w", err)
	}
	if err := s.refresh(ctx); err != nil {
		return sqlc.RefererWhitelist{}, err
	}
	return row, nil
}

// Remove 删除一条规则,成功后即时刷新快照。
// @param ctx 控制超时与取消
// @param id 规则行 id
// @return 被删规则内容(审计留痕用)
// @return ErrPatternNotFound 或数据库错误
func (s *Service) Remove(ctx context.Context, id int64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pattern, err := s.queries.DeleteRefererPattern(ctx, id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", ErrPatternNotFound
	case err != nil:
		return "", fmt.Errorf("删除防盗链规则: %w", err)
	}
	if err := s.refresh(ctx); err != nil {
		return "", err
	}
	return pattern, nil
}

// seed 表为空时把配置种子写入表;每条种子先归一校验。
// @param ctx 控制超时与取消
// @param seeds 种子规则
// @return 种子非法或数据库错误
func (s *Service) seed(ctx context.Context, seeds []string) error {
	if len(seeds) == 0 {
		return nil
	}
	n, err := s.queries.CountRefererWhitelist(ctx)
	if err != nil {
		return fmt.Errorf("统计防盗链规则: %w", err)
	}
	if n > 0 {
		return nil
	}
	for _, raw := range seeds {
		pattern, err := NormalizePattern(raw)
		if err != nil {
			return fmt.Errorf("配置 referer_whitelist 含非法规则 %q: %w", raw, err)
		}
		// OR IGNORE 吸收种子内部重复(如 "A.com" 与 "a.com" 归一后同值)。
		if err := s.queries.SeedRefererPattern(ctx, pattern); err != nil {
			return fmt.Errorf("播种防盗链规则 %q: %w", pattern, err)
		}
	}
	return nil
}

// refresh 从表重建内存快照并原子发布。
// @param ctx 控制超时与取消
// @return 数据库错误
func (s *Service) refresh(ctx context.Context) error {
	rows, err := s.queries.ListRefererWhitelist(ctx)
	if err != nil {
		return fmt.Errorf("装载防盗链规则: %w", err)
	}
	patterns := make([]string, 0, len(rows))
	for _, row := range rows {
		patterns = append(patterns, row.Pattern)
	}
	s.snapshot.Store(&patterns)
	return nil
}
