// Package tags 管理图片标签:标签名的校验与归一、图片上标签的增删改。
// 标签属于图片的属主,同一用户内按名称唯一;写入都在调用方给定的事务里完成。
package tags

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

const (
	// MaxNameRunes 标签名的最大字符数。
	MaxNameRunes = 32
	// MaxPerImage 一张图上标签的最大个数。
	MaxPerImage = 20
)

var (
	// ErrInvalidName 标签名为空、过长或含控制字符。
	ErrInvalidName = errors.New("tags: 标签名非法")
	// ErrTooMany 一张图上的标签超过 MaxPerImage 个。
	ErrTooMany = errors.New("tags: 图片上的标签过多")
	// ErrImageNotFound 目标图片不存在。
	ErrImageNotFound = errors.New("tags: 图片不存在")
)

// Normalize 校验并归一一组标签名:去首尾空白、逐个校验、去重,保持首次出现的顺序。
// @param names 原始标签名
// @return 归一后的标签名
// @return 任一标签名非法时为 ErrInvalidName
func Normalize(names []string) ([]string, error) {
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if !validName(name) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidName, raw)
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out, nil
}

// validName 判断已去空白的标签名是否合法:1 到 MaxNameRunes 个字符,不含控制字符。
// @param name 已去首尾空白的标签名
// @return 合法为 true
func validName(name string) bool {
	n := utf8.RuneCountInString(name)
	return n >= 1 && n <= MaxNameRunes && !strings.ContainsFunc(name, unicode.IsControl)
}

// Change 对一张图标签的修改:Set 非 nil 时整体替换;否则先删 Remove 再加 Add。
type Change struct {
	Set    *[]string // 整体替换为这组标签;nil 表示不替换
	Add    []string  // 追加的标签
	Remove []string  // 移除的标签
}

// Service 标签服务,只持有数据库句柄,并发安全。
type Service struct {
	conn *sql.DB       // 写连接,事务由本服务开启
	q    *sqlc.Queries // 查询集
}

// New 创建标签服务。
// @param conn 写连接
// @param q 查询集
// @return 标签服务
func New(conn *sql.DB, q *sqlc.Queries) *Service {
	return &Service{conn: conn, q: q}
}

// Update 在单事务内修改一张图的标签;标签挂在图片属主名下,不存在的标签随之创建。
// @param ctx 控制超时与取消
// @param imageID 目标图片短 ID
// @param c 修改内容
// @return 图片不存在为 ErrImageNotFound;标签名非法为 ErrInvalidName;超过上限为 ErrTooMany;落库失败时的错误
func (s *Service) Update(ctx context.Context, imageID string, c Change) error {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务: %w", err)
	}
	// 提交成功后 Rollback 返回 ErrTxDone,属预期,显式忽略。
	defer func() { _ = tx.Rollback() }()
	q := s.q.WithTx(tx)
	owner, err := q.GetImageOwner(ctx, imageID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrImageNotFound
	}
	if err != nil {
		return fmt.Errorf("查询图片归属: %w", err)
	}
	if err := apply(ctx, q, owner, imageID, c); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交标签修改: %w", err)
	}
	return nil
}

// plan 归一后的标签修改。
type plan struct {
	clear  bool     // 先清空图片上的全部标签
	remove []string // 移除的标签
	add    []string // 追加的标签
}

// makePlan 校验并归一一次修改:整体替换等于先清空再追加。
// @param c 修改内容
// @return 归一后的修改
// @return 标签名非法时为 ErrInvalidName
func makePlan(c Change) (plan, error) {
	if c.Set != nil {
		set, err := Normalize(*c.Set)
		return plan{clear: true, add: set}, err
	}
	add, err := Normalize(c.Add)
	if err != nil {
		return plan{}, err
	}
	remove, err := Normalize(c.Remove)
	return plan{add: add, remove: remove}, err
}

// apply 把修改施加到一张图上,并核对修改后的标签数不超过上限。
// @param q 事务内查询集
// @param owner 图片属主
// @param imageID 目标图片短 ID
// @param c 修改内容
// @return 标签名非法、超过上限或落库失败时的错误
func apply(ctx context.Context, q *sqlc.Queries, owner, imageID string, c Change) error {
	p, err := makePlan(c)
	if err != nil {
		return err
	}
	if p.clear {
		if err := q.ClearImageTags(ctx, imageID); err != nil {
			return fmt.Errorf("清除图片标签: %w", err)
		}
	}
	for _, name := range p.remove {
		params := sqlc.RemoveImageTagParams{ImageID: imageID, UserID: owner, Name: name}
		if err := q.RemoveImageTag(ctx, params); err != nil {
			return fmt.Errorf("移除标签: %w", err)
		}
	}
	for _, name := range p.add {
		id, err := q.UpsertTag(ctx, sqlc.UpsertTagParams{UserID: owner, Name: name})
		if err != nil {
			return fmt.Errorf("创建标签: %w", err)
		}
		if err := q.AddImageTag(ctx, sqlc.AddImageTagParams{ImageID: imageID, TagID: id}); err != nil {
			return fmt.Errorf("关联标签: %w", err)
		}
	}
	n, err := q.CountImageTags(ctx, imageID)
	if err != nil {
		return fmt.Errorf("统计图片标签: %w", err)
	}
	if n > MaxPerImage {
		return ErrTooMany
	}
	return nil
}

// AddToNewImage 给刚在同一事务里创建的图片挂标签,供上传落库时使用。
// @param q 事务内查询集
// @param owner 图片属主
// @param imageID 新建图片的短 ID
// @param names 标签名
// @return 标签名非法、超过上限或落库失败时的错误
func AddToNewImage(ctx context.Context, q *sqlc.Queries, owner, imageID string, names []string) error {
	return apply(ctx, q, owner, imageID, Change{Add: names})
}
