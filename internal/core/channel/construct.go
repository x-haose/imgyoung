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

// pendingBuilds 到点放弃、仍在后台进行的后端构造,按构造键登记;同一键同时至多一个。
// @Description: 有的后端构造时不理会取消(ftp 等欢迎语、sftp 等 SSH 握手),放弃后协程与连接要等后端自行结束;
// 同一渠道每轮健康检查都重试装载、同一配置可以反复测试连接,不设上限就会一次留下一份。
type pendingBuilds struct {
	mu   sync.Mutex      // 保护 keys
	keys map[string]bool // 仍在进行的构造键
}

// claim 登记一次构造;同键已有构造在进行时不登记。
// @param key 构造键
// @return 登记成功为 true
func (p *pendingBuilds) claim(key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.keys[key] {
		return false
	}
	if p.keys == nil {
		p.keys = map[string]bool{}
	}
	p.keys[key] = true
	return true
}

// release 撤销 claim 的登记。
// @param key 构造键
func (p *pendingBuilds) release(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.keys, key)
}

// constructed 一次后端构造的结果。
type constructed struct {
	d   *rclone.Driver // 驱动;err 非 nil 时为 nil
	err error          // 构造失败的原因
}

// openBackend 在 loadTimeout 内构造驱动;到点仍未构造完即放弃,构造在后台继续、结束后关掉驱动。
// @Description: 时限只管构造,构造好的驱动不受它影响。同一配置上一次放弃的构造还没结束时不再构造,直接按超时
// 失败:后台构造的数目以配置为单位封顶,不随重试累积。
// @param opts 构造参数
// @return 就绪的驱动
// @return 后端初始化失败或超时(包裹 context.DeadlineExceeded)的错误
func (m *Manager) openBackend(ctx context.Context, opts rclone.Options) (*rclone.Driver, error) {
	key, err := buildKey(opts)
	if err != nil {
		return nil, err
	}
	if !m.builds.claim(key) {
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
		m.builds.release(key)
		return c.d, c.err
	case <-ctx.Done():
		go m.discardLate(context.WithoutCancel(ctx), key, done)
		return nil, fmt.Errorf("构造后端未在时限内完成: %w", context.Cause(ctx))
	}
}

// discardLate 等一次已放弃的构造结束,构造成了就关掉驱动,再撤销登记。
// @Description: 退出条件是构造返回,时限由后端自身的超时决定;同一配置至多一个(见 pendingBuilds)。
// @param ctx 关闭驱动所用的上下文,不随调用方取消
// @param key 构造键
// @param done 构造结果
func (m *Manager) discardLate(ctx context.Context, key string, done <-chan constructed) {
	defer m.builds.release(key)
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
