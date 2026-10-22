package replication

import (
	"context"
	"io"

	"golang.org/x/time/rate"
)

// burstBytes 限速令牌桶的突发量,也是单次读取的字节数上限。固定不变:速率可以随配置热改,而突发量不动,
// 就不会出现"在途读取一次读到的字节数超过了被别的任务调小的突发量"。运行配置里的最小限速是 1 MB/s,
// 不小于突发量,所以一次读取最多等一秒。
const burstBytes = 1 << 20

// bandwidth 后台补课的全局带宽限制:所有补课任务共用一个令牌桶,所以限制的是它们合计的写入速率。
type bandwidth struct {
	lim *rate.Limiter // 共用的令牌桶,速率按每次读取时的配置调整
}

// newBandwidth 创建全局带宽限制,初始不限速。
// @return 就绪的带宽限制
func newBandwidth() *bandwidth {
	return &bandwidth{lim: rate.NewLimiter(rate.Inf, burstBytes)}
}

// wrap 包装一个读取流,使它的读取速率受共用的令牌桶约束。上限逐次读取时重新取值:改了配置,在途任务也立即按新值走;
// 取到 0 表示不限,这次读取不经过令牌桶。
// @param ctx 控制等待的取消
// @param r 待限速的读取流
// @param limit 取当前的总带宽上限(字节/秒),0 表示不限
// @return 限速后的读取流
func (b *bandwidth) wrap(ctx context.Context, r io.Reader, limit func() int64) io.Reader {
	return &throttledReader{ctx: ctx, r: r, lim: b.lim, limit: limit}
}

// throttledReader 读多少就向令牌桶取多少额度的读取流。
type throttledReader struct {
	ctx   context.Context // 控制等待额度的取消;读取流的生命周期就是一次补齐任务
	r     io.Reader       // 被限速的读取流
	lim   *rate.Limiter   // 共用的令牌桶
	limit func() int64    // 取当前的总带宽上限(字节/秒),0 表示不限
}

// Read 读取并按读到的字节数等待额度。
// @param p 目标缓冲区,单次最多读 burstBytes 字节
// @return 读到的字节数
// @return 底层读取错误,或等待额度被取消的错误
func (t *throttledReader) Read(p []byte) (int, error) {
	bytesPerSec := t.limit()
	if bytesPerSec <= 0 {
		return t.r.Read(p)
	}
	t.lim.SetLimit(rate.Limit(bytesPerSec))
	if len(p) > burstBytes {
		p = p[:burstBytes]
	}
	n, err := t.r.Read(p)
	if n > 0 {
		if werr := t.lim.WaitN(t.ctx, n); werr != nil {
			return n, werr
		}
	}
	return n, err
}
