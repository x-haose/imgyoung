package tags_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/x-haose/imgyoung/internal/core/tags"
	"github.com/x-haose/imgyoung/internal/infra/db/dbtest"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// TestNormalize 去空白、去重保序;空名、过长、含控制字符的名字整体拒绝。
func TestNormalize(t *testing.T) {
	got, err := tags.Normalize([]string{" blog ", "photo", "blog", "博客"})
	if err != nil || !reflect.DeepEqual(got, []string{"blog", "photo", "博客"}) {
		t.Fatalf("Normalize = %v, %v", got, err)
	}
	for name, in := range map[string][]string{
		"空":    {"  "},
		"过长":   {strings.Repeat("a", tags.MaxNameRunes+1)},
		"控制字符": {"a\x00b"},
		"含换行":  {"a\nb"},
	} {
		if _, err := tags.Normalize(in); !errors.Is(err, tags.ErrInvalidName) {
			t.Errorf("%s: err = %v, 期望 ErrInvalidName", name, err)
		}
	}
	if _, err := tags.Normalize([]string{strings.Repeat("字", tags.MaxNameRunes)}); err != nil {
		t.Errorf("恰好 %d 个字符应通过: %v", tags.MaxNameRunes, err)
	}
}

// newImage 造一张属于 owner 的图。
func newImage(t *testing.T, q *sqlc.Queries, owner, id string) {
	t.Helper()
	blobID, err := q.CreateBlob(t.Context(), sqlc.CreateBlobParams{
		Sha256: "sha-" + id, Phash: 1, StorageKey: "key-" + id, Mime: "image/png", Size: 3, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("建 blob: %v", err)
	}
	if err := q.CreateImage(t.Context(), sqlc.CreateImageParams{ID: id, BlobID: blobID, UserID: owner}); err != nil {
		t.Fatalf("建 image: %v", err)
	}
}

func names(t *testing.T, q *sqlc.Queries, id string) []string {
	t.Helper()
	got, err := q.ListImageTagNames(t.Context(), id)
	if err != nil {
		t.Fatalf("读取标签: %v", err)
	}
	return got
}

// TestUpdate 整体替换、追加与移除;标签挂在图片属主名下;删除图片后关联随之消失;
// 超过上限整体回滚,不留下半截写入。
func TestUpdate(t *testing.T) {
	conn, q := dbtest.New(t)
	svc := tags.New(conn, q)
	ctx := t.Context()
	newImage(t, q, sqlc.DefaultUserID, "img1")

	set := []string{"b", "a"}
	if err := svc.Update(ctx, "img1", tags.Change{Set: &set}); err != nil {
		t.Fatalf("整体替换: %v", err)
	}
	if got := names(t, q, "img1"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("替换后标签 = %v", got)
	}
	if err := svc.Update(ctx, "img1", tags.Change{Add: []string{"c", "a"}, Remove: []string{"b"}}); err != nil {
		t.Fatalf("追加与移除: %v", err)
	}
	if got := names(t, q, "img1"); !reflect.DeepEqual(got, []string{"a", "c"}) {
		t.Fatalf("追加与移除后标签 = %v", got)
	}
	used, err := q.ListUserTags(ctx, sqlc.DefaultUserID)
	if err != nil || len(used) != 2 {
		t.Fatalf("用户标签 = %v, %v, 期望只有仍在使用的 a、c", used, err)
	}

	many := make([]string, tags.MaxPerImage+1)
	for i := range many {
		many[i] = fmt.Sprintf("t%d", i)
	}
	if err := svc.Update(ctx, "img1", tags.Change{Add: many}); !errors.Is(err, tags.ErrTooMany) {
		t.Fatalf("超过上限: err = %v, 期望 ErrTooMany", err)
	}
	if got := names(t, q, "img1"); !reflect.DeepEqual(got, []string{"a", "c"}) {
		t.Fatalf("超限的修改没有整体回滚: %v", got)
	}
	if err := svc.Update(ctx, "nope", tags.Change{Add: []string{"x"}}); !errors.Is(err, tags.ErrImageNotFound) {
		t.Fatalf("不存在的图片: err = %v", err)
	}

	if _, err := q.DeleteImage(ctx, "img1"); err != nil {
		t.Fatalf("删除图片: %v", err)
	}
	if got := names(t, q, "img1"); len(got) != 0 {
		t.Fatalf("删除图片后标签关联仍在: %v", got)
	}
}
