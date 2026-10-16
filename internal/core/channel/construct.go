package channel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/x-haose/imgyoung/store/rclone"
)

// pendingBuilds 到点放弃、仍在后台进行的后端构造,按构造键计数。
// @Description: 有的后端构造时不理会取消(ftp 等欢迎语、sftp 等 SSH 握手),放弃后协程与连接要等后端自行结束;
// 同一渠道每轮健康检查都重试装载、同一配置可以反复测试连接,不设上限就会一次留下一份。只计已放弃的构造:
// 正常进行中的同配置构造(如健康检查重试装载时恰好点了测试连接)互不影响。
type pendingBuilds struct {
	mu   sync.Mutex     // 保护 keys
	keys map[string]int // 构造键 → 已放弃、仍在进行的构造数
}

// busy 报告同一构造键是否有已放弃、仍在进行的构造。
// @param key 构造键
// @return 有为 true
func (p *pendingBuilds) busy(key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.keys[key] > 0
}

// add 登记一次放弃的构造。
// @param key 构造键
func (p *pendingBuilds) add(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.keys == nil {
		p.keys = map[string]int{}
	}
	p.keys[key]++
}

// done 撤销 add 的一次登记。
// @param key 构造键
func (p *pendingBuilds) done(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.keys[key]--; p.keys[key] <= 0 {
		delete(p.keys, key)
	}
}

// constructed 一次后端构造的结果。
type constructed struct {
	d   *rclone.Driver // 驱动;err 非 nil 时为 nil
	err error          // 构造失败的原因
}

// openBackend 在 loadTimeout 内构造驱动;到点仍未构造完即放弃,构造在后台继续、结束后关掉驱动。
// @Description: 时限只管构造,构造好的驱动不受它影响。同一配置上一次放弃的构造还没结束时不再构造,直接按超时
// 失败:放弃后仍在后台的构造,每个配置至多是放弃前已同时开始的那几个,不随重试累积。
// @param opts 构造参数
// @return 就绪的驱动
// @return 后端初始化失败或超时(包裹 context.DeadlineExceeded)的错误
func (m *Manager) openBackend(ctx context.Context, opts rclone.Options) (*rclone.Driver, error) {
	key, err := buildKey(opts)
	if err != nil {
		return nil, err
	}
	if m.builds.busy(key) {
		return nil, fmt.Errorf("构造后端未在时限内完成(同一配置上一次构造仍未结束): %w", context.DeadlineExceeded)
	}
	ctx, cancel := context.WithTimeout(ctx, loadTimeout)
	defer cancel()
	done := make(chan constructed, 1)
	go func() {
		d, err := rclone.Open(ctx, opts)
		done <- constructed{d: d, err: err}
	}()
	select {
	case c := <-done:
		return c.d, c.err
	case <-ctx.Done():
		m.builds.add(key)
		go m.discardLate(context.WithoutCancel(ctx), key, done)
		return nil, fmt.Errorf("构造后端未在时限内完成: %w", context.Cause(ctx))
	}
}

// discardLate 等一次已放弃的构造结束,构造成了就关掉驱动,再撤销登记。
// @Description: 退出条件是构造返回,时限由后端自身的超时决定;数目见 pendingBuilds。
// @param ctx 关闭驱动所用的上下文,不随调用方取消
// @param key 构造键
// @param done 构造结果
func (m *Manager) discardLate(ctx context.Context, key string, done <-chan constructed) {
	defer m.builds.done(key)
	c := <-done
	if c.err != nil {
		return
	}
	if err := c.d.Close(ctx); err != nil {
		m.cfg.Logger.Warn("关闭超时放弃的驱动失败", "err", err)
	}
}

// buildKey 由构造参数得出构造键:后端名、参数、路径与 crypt 参数完全相同即同一配置。
// @param opts 构造参数
// @return 构造键(参数的 SHA-256,不含明文)
// @return 编码失败时的错误
func buildKey(opts rclone.Options) (string, error) {
	raw, err := json.Marshal(struct {
		Backend string
		Params  map[string]string
		Path    string
		Crypt   *rclone.CryptOptions
	}{opts.Backend, opts.Params, opts.Path, opts.Crypt})
	if err != nil {
		return "", fmt.Errorf("编码构造参数: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
