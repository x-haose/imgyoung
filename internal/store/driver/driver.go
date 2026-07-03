// Package driver 定义 StorageDriver 抽象层(设计定稿·StorageDriver 抽象层)。
//
// 抽象层只负责"把字节存到某后端 / 取回 / 查存在性 / 删除",
// 不承载任何业务语义(去重、TTL、主备等全部在 core 层);且不 import 任何具体后端,
// 是可复用存储引擎的接口缝——rclone.Driver 等实现依赖本包,而本包不反向依赖它们
// (设计定稿·7.4 库化边界)。
package driver

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrNotFound 表示目标对象在该渠道中不存在。
// @Description: 所有 Driver 实现在对象缺失时必须返回可被 errors.Is 命中的该哨兵。
var ErrNotFound = errors.New("storage: object not found")

// ObjectInfo 渠道内对象的基本信息。
type ObjectInfo struct {
	// Key 渠道内对象键(相对路径形式)。
	Key string
	// Size 对象字节大小。
	Size int64
	// ModTime 对象修改时间(对账宽限期与备份保留判断依据)。
	ModTime time.Time
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
}
