// Package failover 在一组驱动之上提供无状态的容灾原语:按序写够 N 份、按序读第一个可读的、健康探测。
//
// 驱动切片的顺序即优先级,本包不持有任何状态——哪些驱动参与、谁健康、对象在哪几个驱动上有副本,
// 都由调用方决定后按优先级排好传入。只依赖 driver 接口,不引入任何具体后端。
//
// 本包对外公开,导出标识符在 v1 主版本内保持向后兼容。
package failover

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/x-haose/imgyoung/store/driver"
)

var (
	// ErrNoDrivers 传入的驱动切片为空。
	ErrNoDrivers = errors.New("failover: 没有可用的驱动")
	// ErrInvalidNeed 要求的成功份数小于 1。
	ErrInvalidNeed = errors.New("failover: 成功份数须 >= 1")
	// ErrInsufficient 全部驱动都试过,成功写入的份数仍不足要求。
	ErrInsufficient = errors.New("failover: 成功写入的份数不足")
	// ErrUnavailable 没有驱动能读出对象,且至少一个驱动的失败不是"对象不存在"。
	ErrUnavailable = errors.New("failover: 没有驱动能读出对象")
)

// Attempt 对单个驱动的一次尝试。
type Attempt struct {
	Index int   // 驱动在传入切片中的下标
	Err   error // nil 表示成功
}

// WriteOptions 写入策略。
type WriteOptions struct {
	Need           int           // 最少成功份数,>= 1;凑够即停,其余驱动不再写
	AttemptTimeout time.Duration // 单个驱动一次写入的超时;0 表示只受 ctx 约束
}

// Write 按顺序向驱动写入同一份数据,凑够 opts.Need 份即返回;某个驱动写失败就顺延下一个。
// @Description: 每次尝试都从 r 的开头重新读,失败的半截写入不影响下一个驱动。ctx 取消后不再
// 尝试后续驱动,直接返回取消错误——取消造成的失败不是驱动的结论。
// @param ds 驱动,按优先级降序
// @param key 对象键
// @param r 数据源,每次尝试独立读取 [0, size)
// @param size 数据字节数
// @param opts 写入策略
// @return 实际发生的尝试,按尝试顺序;成功者即持有副本的驱动
// @return ErrNoDrivers、ErrInvalidNeed、ctx 取消错误,或份数不足时包裹 ErrInsufficient 与各次失败原因的错误
func Write(ctx context.Context, ds []driver.Driver, key string, r io.ReaderAt, size int64,
	opts WriteOptions) ([]Attempt, error) {
	if opts.Need < 1 {
		return nil, fmt.Errorf("%w: 实际 %d", ErrInvalidNeed, opts.Need)
	}
	if len(ds) == 0 {
		return nil, ErrNoDrivers
	}
	attempts := make([]Attempt, 0, opts.Need)
	var failures []error
	done := 0
	for i, d := range ds {
		if done >= opts.Need {
			break
		}
		if err := ctx.Err(); err != nil {
			return attempts, err
		}
		err := put(ctx, d, key, io.NewSectionReader(r, 0, size), size, opts.AttemptTimeout)
		attempts = append(attempts, Attempt{Index: i, Err: err})
		if err != nil {
			failures = append(failures, fmt.Errorf("驱动 %d: %w", i, err))
			continue
		}
		done++
	}
	if done < opts.Need {
		return attempts, fmt.Errorf("%w: 要求 %d 份,成功 %d 份: %w",
			ErrInsufficient, opts.Need, done, errors.Join(failures...))
	}
	return attempts, nil
}

// put 向单个驱动写入一次,可选独立超时。
// @param d 目标驱动
// @param key 对象键
// @param r 本次尝试的数据流
// @param size 数据字节数
// @param timeout 本次写入的超时;0 表示只受 ctx 约束
// @return 写入失败或超时时的错误
func put(ctx context.Context, d driver.Driver, key string, r io.Reader, size int64, timeout time.Duration) error {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	return d.Put(ctx, key, r, size)
}

// Open 按顺序打开对象,返回第一个成功的读取流。
// @Description: 打开成功即返回,后续驱动不再访问。全部失败时两类错误互斥:每个驱动都报告对象不存在,
// 返回包裹 driver.ErrNotFound 的错误(对象确实缺失);否则返回包裹 ErrUnavailable 的错误
// (至少一个驱动故障,对象可能仍在),此时各驱动的原始错误只以文本出现,要逐个判断请看尝试记录。
// ctx 取消后不再尝试后续驱动。
// @param ds 驱动,按优先级降序
// @param key 对象键
// @return 读取流,调用方负责 Close;失败时为 nil
// @return 实际发生的尝试,按尝试顺序;成功时最后一项即提供读取流的驱动
// @return ErrNoDrivers、ctx 取消错误,或上述两类全部失败错误
func Open(ctx context.Context, ds []driver.Driver, key string) (io.ReadCloser, []Attempt, error) {
	if len(ds) == 0 {
		return nil, nil, ErrNoDrivers
	}
	attempts := make([]Attempt, 0, 1)
	for i, d := range ds {
		if err := ctx.Err(); err != nil {
			return nil, attempts, err
		}
		rc, err := d.Open(ctx, key)
		attempts = append(attempts, Attempt{Index: i, Err: err})
		if err == nil {
			return rc, attempts, nil
		}
	}
	return nil, attempts, unreadable(key, attempts)
}

// unreadable 把全部失败的读取尝试归结为一个错误:全是"不存在"即对象缺失,否则为不可读。
// @Description: 不可读时各驱动的错误只拼进文本、不参与包裹——其中可能有别的驱动报的"不存在",
// 包裹进来会让 errors.Is(err, driver.ErrNotFound) 也成立,把"有驱动故障"误判成"对象缺失"。
// @param key 对象键
// @param attempts 全部失败的尝试
// @return 包裹 driver.ErrNotFound 或 ErrUnavailable(二者互斥)的错误
func unreadable(key string, attempts []Attempt) error {
	reasons := make([]string, 0, len(attempts))
	missing := 0
	for _, a := range attempts {
		if errors.Is(a.Err, driver.ErrNotFound) {
			missing++
		}
		reasons = append(reasons, fmt.Sprintf("驱动 %d: %v", a.Index, a.Err))
	}
	if missing == len(attempts) {
		return fmt.Errorf("对象 %q 在 %d 个驱动上均不存在: %w", key, missing, driver.ErrNotFound)
	}
	return fmt.Errorf("%w: 对象 %q: %s", ErrUnavailable, key, strings.Join(reasons, "; "))
}
