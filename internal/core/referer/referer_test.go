package referer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/x-haose/imgyoung/internal/infra/db/dbtest"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// newQueries 打开并迁移一个临时 SQLite 库,返回查询集。
func newQueries(t *testing.T) *sqlc.Queries {
	t.Helper()
	_, q := dbtest.New(t)
	return q
}

// TestNormalizePattern 表驱动验证规则语法:裸域与子域通配合法,
// scheme/路径/端口/裸通配等一律拒绝,输出统一小写。
func TestNormalizePattern(t *testing.T) {
	cases := map[string]struct {
		raw     string
		want    string
		wantErr bool
	}{
		"裸域":      {raw: "example.com", want: "example.com"},
		"子域通配":    {raw: "*.example.com", want: "*.example.com"},
		"单段主机":    {raw: "localhost", want: "localhost"},
		"大写归一":    {raw: " *.Example.COM ", want: "*.example.com"},
		"空串":      {raw: "  ", wantErr: true},
		"带scheme": {raw: "https://example.com", wantErr: true},
		"带路径":     {raw: "example.com/img", wantErr: true},
		"带端口":     {raw: "example.com:8080", wantErr: true},
		"裸通配":     {raw: "*", wantErr: true},
		"中缀通配":    {raw: "a.*.com", wantErr: true},
		"段首连字符":   {raw: "-a.example.com", wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := NormalizePattern(tc.raw)
			if tc.wantErr {
				if !errors.Is(err, ErrPatternInvalid) {
					t.Fatalf("NormalizePattern(%q) err = %v, 期望 ErrPatternInvalid", tc.raw, err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("NormalizePattern(%q) = (%q, %v), 期望 (%q, nil)", tc.raw, got, err, tc.want)
			}
		})
	}
}

// TestAddRemoveRefreshSnapshot 验证增删即时刷新快照,且哨兵错误语义正确。
func TestAddRemoveRefreshSnapshot(t *testing.T) {
	ctx := context.Background()
	svc, err := New(ctx, newQueries(t))
	if err != nil {
		t.Fatalf("构造服务: %v", err)
	}
	if got := svc.Patterns(); len(got) != 0 {
		t.Fatalf("初始规则 = %v, 期望为空", got)
	}

	row, err := svc.Add(ctx, "*.OK.com")
	if err != nil {
		t.Fatalf("添加规则: %v", err)
	}
	if row.Pattern != "*.ok.com" {
		t.Fatalf("落库规则 = %q, 期望归一为 *.ok.com", row.Pattern)
	}
	if got := svc.Patterns(); len(got) != 1 || got[0] != "*.ok.com" {
		t.Fatalf("添加后快照 = %v, 期望 [*.ok.com]", got)
	}

	if _, err := svc.Add(ctx, "*.ok.com"); !errors.Is(err, ErrPatternExists) {
		t.Fatalf("重复添加 err = %v, 期望 ErrPatternExists", err)
	}
	if _, err := svc.Add(ctx, "bad host"); !errors.Is(err, ErrPatternInvalid) {
		t.Fatalf("非法规则 err = %v, 期望 ErrPatternInvalid", err)
	}

	pattern, err := svc.Remove(ctx, row.ID)
	if err != nil || pattern != "*.ok.com" {
		t.Fatalf("删除规则 = (%q, %v), 期望 (*.ok.com, nil)", pattern, err)
	}
	if got := svc.Patterns(); len(got) != 0 {
		t.Fatalf("删除后快照 = %v, 期望为空", got)
	}
	if _, err := svc.Remove(ctx, row.ID); !errors.Is(err, ErrPatternNotFound) {
		t.Fatalf("删除不存在规则 err = %v, 期望 ErrPatternNotFound", err)
	}
}

// TestConcurrentWritesPublishConsistentSnapshot 验证并发增删收敛后快照与表完全一致:
// 写路径互斥保证快照发布顺序与 DB 变更顺序一致,不残留过期快照。
func TestConcurrentWritesPublishConsistentSnapshot(t *testing.T) {
	ctx := context.Background()
	svc, err := New(ctx, newQueries(t))
	if err != nil {
		t.Fatalf("构造服务: %v", err)
	}

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			pattern := fmt.Sprintf("h%d.example.com", i)
			row, err := svc.Add(ctx, pattern)
			if err != nil {
				t.Errorf("并发添加 %s: %v", pattern, err)
				return
			}
			// 偶数序号加完即删,制造增删交错。
			if i%2 == 0 {
				if _, err := svc.Remove(ctx, row.ID); err != nil {
					t.Errorf("并发删除 %s: %v", pattern, err)
				}
			}
		})
	}
	wg.Wait()

	rows, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("查询规则: %v", err)
	}
	inDB := make(map[string]bool, len(rows))
	for _, row := range rows {
		inDB[row.Pattern] = true
	}
	got := svc.Patterns()
	if len(got) != len(inDB) {
		t.Fatalf("快照 %v 与表 %d 条不一致", got, len(inDB))
	}
	for _, p := range got {
		if !inDB[p] {
			t.Fatalf("快照含表中不存在的规则 %q", p)
		}
	}
}
