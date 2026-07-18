// Package rclone 以 rclone 的 fs.Fs 实现 StorageDriver。
// 依赖 driver 包的接口,是可选(opt-in)后端:仅 import 本包才会拖入 rclone 依赖树。
package rclone

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/object"
	"github.com/rclone/rclone/fs/walk"

	"github.com/x-haose/imgyoung/internal/store/driver"

	// 按需注册 rclone 后端:
	// local 为内置渠道;crypt 供渠道存储加密包裹任意后端;
	// s3(覆盖 AWS/R2/MinIO 等兼容服务)与 webdav 为最常用的远端类型。
	_ "github.com/rclone/rclone/backend/crypt"
	_ "github.com/rclone/rclone/backend/local"
	_ "github.com/rclone/rclone/backend/s3"
	_ "github.com/rclone/rclone/backend/webdav"
)

// Driver 基于 rclone 路线一(直接 import fs 包)的 Driver 实现。
// @Description: 通过 fs.Fs 流式读写,不落盘、不依赖 rclone 配置文件;
// remote 以连接字符串形式在运行时动态注入。
// fs.Fs 本身并发安全,故本类型无内部状态锁。
type Driver struct {
	f fs.Fs // 已初始化的 rclone 文件系统句柄
}

// 编译期断言:Driver 必须完整实现 Driver 接口。
var _ driver.Driver = (*Driver)(nil)

// New 以 remote 连接字符串创建 Driver。
// @Description: 错误链不携带 remote 原文——连接字符串内联凭证,任何回显
// (日志、API 错误消息)都是凭证泄露面。
// @param remote rclone 连接字符串(本地路径或 ":backend,opt=v:path" 形式)
// @return 创建好的驱动实例
// @return remote 无法解析或后端初始化失败时的错误
func New(ctx context.Context, remote string) (*Driver, error) {
	f, err := fs.NewFs(ctx, remote)
	if err != nil {
		return nil, fmt.Errorf("初始化 rclone 后端: %w", err)
	}
	return &Driver{f: f}, nil
}

// EnsureBucket 创建渠道根对应的桶/容器,已存在时不报错。
// @Description: 对象存储后端(s3 等)的 Mkdir 在桶层即 CreateBucket。注意:调用方必须
// 用未设 no_check_bucket=true 的实例,否则 s3 后端会短路 Mkdir 使本方法成为空操作。
// 不进 driver.Driver 接口——仅对象存储后端有意义,避免污染核心抽象。
// @return 建桶失败(多为凭证无建桶权限)时的错误
func (d *Driver) EnsureBucket(ctx context.Context) error {
	if err := d.f.Mkdir(ctx, ""); err != nil {
		return fmt.Errorf("创建存储桶: %w", err)
	}
	return nil
}

// Put 将 r 中的 size 个字节流式写入 key。
func (d *Driver) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	src := object.NewStaticObjectInfo(key, time.Now(), size, true, nil, nil)
	if _, err := d.f.Put(ctx, r, src); err != nil {
		return fmt.Errorf("写入对象 %q: %w", key, err)
	}
	return nil
}

// Open 打开 key 对应对象的读取流。
func (d *Driver) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := d.object(ctx, key)
	if err != nil {
		return nil, err
	}
	rc, err := obj.Open(ctx)
	if err != nil {
		return nil, fmt.Errorf("打开对象 %q: %w", key, err)
	}
	return rc, nil
}

// Stat 查询 key 对应对象的信息。
func (d *Driver) Stat(ctx context.Context, key string) (*driver.ObjectInfo, error) {
	obj, err := d.object(ctx, key)
	if err != nil {
		return nil, err
	}
	return &driver.ObjectInfo{Key: key, Size: obj.Size(), ModTime: obj.ModTime(ctx)}, nil
}

// Delete 删除 key 对应的对象。
func (d *Driver) Delete(ctx context.Context, key string) error {
	obj, err := d.object(ctx, key)
	if err != nil {
		return err
	}
	if err := obj.Remove(ctx); err != nil {
		return fmt.Errorf("删除对象 %q: %w", key, err)
	}
	return nil
}

// List 递归列举 dir 目录下的全部对象(dir 为空表示整个渠道)。
func (d *Driver) List(ctx context.Context, dir string) ([]driver.ObjectInfo, error) {
	var out []driver.ObjectInfo
	err := walk.ListR(ctx, d.f, dir, false, -1, walk.ListObjects, func(entries fs.DirEntries) error {
		entries.ForObject(func(o fs.Object) {
			out = append(out, driver.ObjectInfo{
				Key:     o.Remote(),
				Size:    o.Size(),
				ModTime: o.ModTime(ctx),
			})
		})
		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrorDirNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("列举目录 %q: %w", dir, err)
	}
	return out, nil
}

// object 查找 key 对应的 rclone 对象,并把"不存在"统一映射为 driver.ErrNotFound。
// @Description: rclone 对缺失对象可能返回 ErrorObjectNotFound 或 ErrorDirNotFound
// (取决于缺失层级),对调用方统一收敛为本包哨兵。
func (d *Driver) object(ctx context.Context, key string) (fs.Object, error) {
	obj, err := d.f.NewObject(ctx, key)
	if err != nil {
		if errors.Is(err, fs.ErrorObjectNotFound) || errors.Is(err, fs.ErrorDirNotFound) {
			return nil, fmt.Errorf("对象 %q: %w", key, driver.ErrNotFound)
		}
		return nil, fmt.Errorf("查找对象 %q: %w", key, err)
	}
	return obj, nil
}
