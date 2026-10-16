// Package driver 定义存储驱动接口 Driver。
//
// 驱动只负责把字节存到某个后端、取回、查询、列举与删除,不承载去重、有效期、主备切换等业务语义,
// 这些由使用方实现。本包不导入任何具体后端:rclone.Driver 等实现依赖本包,本包不依赖它们。
//
// 本包对外公开,遵循语义化版本:v1.0.0 之前可能有不兼容的改动,v1 起导出标识符在同一主版本内保持向后兼容。
package driver

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrNotFound 表示目标对象在该渠道中不存在。
// @Description: 所有 Driver 实现在对象缺失时必须返回可被 errors.Is 命中的该哨兵。
var ErrNotFound = errors.New("storage: 对象不存在")

// ObjectInfo 渠道内对象的基本信息。
type ObjectInfo struct {
	Key     string    // 渠道内对象键(相对路径形式)
	Size    int64     // 对象字节大小
	ModTime time.Time // 对象修改时间,对账宽限期的判断依据;rclone 驱动在后端支持时取服务端记下的写入时刻
}

// Driver 存储执行器抽象。
// @Description: 实现必须并发安全;所有方法都应尊重 ctx 的取消与超时。
type Driver interface {
	// Put 将 r 中的 size 个字节流式写入 key。
	// @param key 渠道内对象键
	// @param r 数据流,由调用方负责其生命周期
	// @param size 数据字节数(上传链路在内存中缓冲,大小恒为已知)
	Put(ctx context.Context, key string, r io.Reader, size int64) error

	// Open 打开 key 对应对象的读取流。
	// @return 对象数据流,调用方负责 Close;对象缺失时返回 ErrNotFound
	Open(ctx context.Context, key string) (io.ReadCloser, error)

	// Stat 查询 key 对应对象的信息。
	// @return 对象信息;对象缺失时返回 ErrNotFound
	Stat(ctx context.Context, key string) (*ObjectInfo, error)

	// Delete 删除 key 对应的对象。
	// @Description: 删除不存在的对象返回 ErrNotFound,由调用方决定是否视为成功。
	Delete(ctx context.Context, key string) error

	// List 递归列举 dir 目录下的全部对象(dir 为空表示整个渠道)。
	// @Description: 返回键为相对渠道根的完整路径;目录不存在视为空。
	// 供备份保留与对账 GC 使用,属低频操作。
	List(ctx context.Context, dir string) ([]ObjectInfo, error)

	// ListShallow 只列举 dir 这一层的对象,不进入子目录(dir 为空表示渠道根)。
	// @Description: 返回键为相对渠道根的完整路径;目录不存在视为空。列举量不随子目录里的对象数增长,
	// 适合只关心某一层的场合。
	ListShallow(ctx context.Context, dir string) ([]ObjectInfo, error)
}
