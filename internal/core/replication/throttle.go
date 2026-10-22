package replication

import (
	"context"
	"io"

	"golang.org/x/time/rate"
)

const (
	// minBurst 限速令牌桶的最小突发量(字节);再小则每次读取都要拆成很多次等待。
	minBurst = 64 << 10
	// maxBurst 限速令牌桶的最大突发量(字节):限速很高时也不放任一次读取占去过多额度。
	maxBurst = 4 << 20
)

// bandwidth 后台补齐的全局带宽限制:所有补齐任务共用一个令牌桶,所以限制的是各渠道合计的写入速率。
// 上限逐次从配置取,改了即生效;0 表示不限。
type bandwidth struct {
	lim *rate.Limiter // 共用的令牌桶,速率与突发量按每次读到的配置调整
}

// newBandwidth 创建全局带宽限制,初始不限速。
// @return 就绪的带宽限制
func newBandwidth() *bandwidth {
	return &bandwidth{lim: rate.NewLimiter(rate.Inf, minBurst)}
}

// wrap 按当前上限包装一个读取流:上限为 0 时原样返回,否则读取速率受共用的令牌桶约束。
// @param ctx 控制等待的取消;ctx 截止前凑不够额度时读取返回错误
// @param r 待限速的读取流
// @param bytesPerSec 总带宽上限(字节/秒),0 表示不限
// @return 限速后的读取流
func (b *bandwidth) wrap(ctx context.Context, r io.Reader, bytesPerSec int64) io.Reader {
	if bytesPerSec <= 0 {
		return r
	}
	burst := int(min(max(bytesPerSec, minBurst), maxBurst))
	b.lim.SetLimit(rate.Limit(bytesPerSec))
	b.lim.SetBurst(burst)
	return &throttledReader{ctx: ctx, r: r, lim: b.lim, burst: burst}
}

// throttledReader 读多少就向令牌桶取多少额度的读取流。
type throttledReader struct {
	ctx   context.Context // 控制等待额度的取消;读取流的生命周期就是一次补齐任务
	r     io.Reader       // 被限速的读取流
	lim   *rate.Limiter   // 共用的令牌桶
	burst int             // 单次读取的字节数上限,不超过令牌桶的突发量
}

// Read 读取并按读到的字节数等待额度。
// @param p 目标缓冲区,单次最多读 burst 字节
// @return 读到的字节数
// @return 底层读取错误,或等待额度被取消 / 来不及时的错误
func (t *throttledReader) Read(p []byte) (int, error) {
	if len(p) > t.burst {
		p = p[:t.burst]
	}
	n, err := t.r.Read(p)
	if n > 0 {
		if werr := t.lim.WaitN(t.ctx, n); werr != nil {
			return n, werr
		}
	}
	return n, err
}
