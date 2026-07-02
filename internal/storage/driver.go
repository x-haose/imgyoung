// Package storage 定义 StorageDriver 抽象层及其实现(设计定稿·StorageDriver 抽象层)。
//
// 抽象层只负责"把字节存到某后端 / 取回 / 查存在性 / 删除",
// 不承载任何业务语义(去重、TTL、主备等全部在 Service 层)。
// 接口设计不假设 rclone 存在:未来的原生 driver(S3 直连、第三方图床 API)与
// RcloneDriver 并存(设计定稿·两条升级缝之外的既定扩展点)。
package storage

import (
	"context"
	"errors"
	"io"
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
}
