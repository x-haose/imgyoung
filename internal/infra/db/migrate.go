package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"ariga.io/atlas/sql/migrate"
	"ariga.io/atlas/sql/sqlite"
)

// migrationsFS 打进二进制的 versioned 迁移目录(Atlas 生成的 SQL + atlas.sum)。
//
//go:embed migrations
var migrationsFS embed.FS

// migrationsDir 内嵌目录中迁移文件所在的子目录名。
const migrationsDir = "migrations"

// Migrate 将数据库 schema 迁移到内嵌迁移目录的最新版本。
// @Description: 用 Atlas Executor 顺序应用所有未记录的迁移,幂等;每个版本在
// schema_revisions 表留痕。启动时调用,跨版本升级会自动补齐中间所有版本。
// 应用前校验 atlas.sum,防止迁移文件与校验和不一致。
// @param d 已打开的数据库句柄
// @return 迁移失败时的错误
func Migrate(d *DB) error {
	ctx := context.Background()
	// 迁移、版本表与基线播种全钉在写句柄的唯一连接上:迁移脚本内的 PRAGMA(如
	// foreign_keys=off)只对当前连接生效;写句柄只有这一条连接,期间任何经池的写都会
	// 等它释放,故不能再借池执行。
	c, err := d.write.Conn(ctx)
	if err != nil {
		return fmt.Errorf("获取迁移专用连接: %w", err)
	}
	defer func() { _ = c.Close() }()
	drv, err := sqlite.Open(c)
	if err != nil {
		return fmt.Errorf("初始化迁移驱动: %w", err)
	}
	dir, err := embeddedDir()
	if err != nil {
		return err
	}
	if err := migrate.Validate(dir); err != nil {
		return fmt.Errorf("迁移目录校验(atlas.sum 不匹配): %w", err)
	}
	rrw, err := newRevisions(ctx, c)
	if err != nil {
		return err
	}
	ex, err := migrate.NewExecutor(drv, dir, rrw)
	if err != nil {
		return fmt.Errorf("构造迁移执行器: %w", err)
	}
	pending, err := ex.Pending(ctx)
	switch {
	case errors.Is(err, migrate.ErrNoPendingFiles):
		// schema 已最新,无需应用,仍确保基线数据存在。
	case err != nil:
		return fmt.Errorf("检查待应用迁移: %w", err)
	default:
		for _, f := range pending {
			if err := ex.Execute(ctx, f); err != nil {
				return fmt.Errorf("应用迁移 %s: %w", f.Name(), err)
			}
		}
	}
	return seedDefaults(ctx, c)
}

// embeddedDir 把内嵌的迁移文件(含 atlas.sum)装入内存目录供 Executor 读取。
// @return 就绪的迁移目录
// @return 读取内嵌文件失败时的错误
func embeddedDir() (migrate.Dir, error) {
	dir := migrate.OpenMemDir(migrationsDir)
	entries, err := fs.ReadDir(migrationsFS, migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("读取内嵌迁移目录: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := migrationsFS.ReadFile(migrationsDir + "/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("读取迁移文件 %s: %w", e.Name(), err)
		}
		if err := dir.WriteFile(e.Name(), b); err != nil {
			return nil, fmt.Errorf("装载迁移文件 %s: %w", e.Name(), err)
		}
	}
	return dir, nil
}
