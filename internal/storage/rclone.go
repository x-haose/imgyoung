package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/object"
	"github.com/rclone/rclone/fs/walk"

	// 按需注册 rclone 后端(设计定稿·按需 import 后端):
	// local 为内置渠道;crypt 供渠道存储加密包裹任意后端;
	// s3(覆盖 AWS/R2/MinIO 等兼容服务)与 webdav 为最常用的远端类型。
	_ "github.com/rclone/rclone/backend/crypt"
	_ "github.com/rclone/rclone/backend/local"
	_ "github.com/rclone/rclone/backend/s3"
	_ "github.com/rclone/rclone/backend/webdav"
)

// RcloneDriver 基于 rclone 路线一(直接 import fs 包)的 Driver 实现。
// @Description: 通过 fs.Fs 流式读写,不落盘、不依赖 rclone 配置文件;
// remote 以连接字符串形式在运行时动态注入(设计定稿·配置动态注入)。
// fs.Fs 本身并发安全,故本类型无内部状态锁。
type RcloneDriver struct {
	// f 已初始化的 rclone 文件系统句柄。
	f fs.Fs
}

// 编译期断言:RcloneDriver 必须完整实现 Driver 接口。
var _ Driver = (*RcloneDriver)(nil)

// NewRcloneDriver 以 remote 连接字符串创建 RcloneDriver。
// @param remote rclone 连接字符串(本地路径或 ":backend,opt=v:path" 形式)
// @return 创建好的驱动实例
// @return remote 无法解析或后端初始化失败时的错误
func NewRcloneDriver(ctx context.Context, remote string) (*RcloneDriver, error) {
	f, err := fs.NewFs(ctx, remote)
	if err != nil {
		return nil, fmt.Errorf("初始化 rclone 后端 %q: %w", remote, err)
	}
	return &RcloneDriver{f: f}, nil
}

// Put 将 r 中的 size 个字节流式写入 key。
func (d *RcloneDriver) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	src := object.NewStaticObjectInfo(key, time.Now(), size, true, nil, nil)
	if _, err := d.f.Put(ctx, r, src); err != nil {
		return fmt.Errorf("写入对象 %q: %w", key, err)
	}
	return nil
}

// Open 打开 key 对应对象的读取流。
func (d *RcloneDriver) Open(ctx context.Context, key string) (io.ReadCloser, error) {
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
func (d *RcloneDriver) Stat(ctx context.Context, key string) (*ObjectInfo, error) {
	obj, err := d.object(ctx, key)
	if err != nil {
		return nil, err
	}
	return &ObjectInfo{Key: key, Size: obj.Size()}, nil
}

// Delete 删除 key 对应的对象。
func (d *RcloneDriver) Delete(ctx context.Context, key string) error {
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
func (d *RcloneDriver) List(ctx context.Context, dir string) ([]ObjectInfo, error) {
	var out []ObjectInfo
	err := walk.ListR(ctx, d.f, dir, false, -1, walk.ListObjects, func(entries fs.DirEntries) error {
		entries.ForObject(func(o fs.Object) {
			out = append(out, ObjectInfo{
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

// object 查找 key 对应的 rclone 对象,并把"不存在"统一映射为 ErrNotFound。
// @Description: rclone 对缺失对象可能返回 ErrorObjectNotFound 或 ErrorDirNotFound
// (取决于缺失层级),对调用方统一收敛为本包哨兵。
func (d *RcloneDriver) object(ctx context.Context, key string) (fs.Object, error) {
	obj, err := d.f.NewObject(ctx, key)
	if err != nil {
		if errors.Is(err, fs.ErrorObjectNotFound) || errors.Is(err, fs.ErrorDirNotFound) {
			return nil, fmt.Errorf("对象 %q: %w", key, ErrNotFound)
		}
		return nil, fmt.Errorf("查找对象 %q: %w", key, err)
	}
	return obj, nil
}
