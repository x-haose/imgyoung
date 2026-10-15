package reliability

import (
	"context"
	"errors"
)

// ErrStopped 备份服务已停止(停机中),不再接受手动备份与恢复。
var ErrStopped = errors.New("reliability: 备份服务已停止")

// job 一项交给 Run 执行的手动操作。
type job struct {
	run  func(ctx context.Context) // 操作本体;ctx 为服务的生命周期上下文
	done chan struct{}             // 操作结束时关闭
}

// Backup 立即备份一次(见 RunOnce),由 Run 执行。
// @Description: 手动备份归备份服务所有、不归发起它的请求:客户端断开不中止它,停机时随服务的生命周期上下文取消,
// 写了一半的快照由后端删除,Run 等它结束才返回。本方法一直等到备份结束。
// @return 新快照;失败时为零值
// @return RunOnce 的错误;服务已停止时为 ErrStopped
func (s *Service) Backup() (Snapshot, error) {
	var (
		snap Snapshot
		err  error
	)
	if serr := s.submit(func(ctx context.Context) { snap, err = s.RunOnce(ctx) }); serr != nil {
		return Snapshot{}, serr
	}
	return snap, err
}

// Restore 把一份快照暂存到数据目录(见 stage),由 Run 执行;归属与取消语义同 Backup。
// @param name 快照名(List 返回的 Name)
// @return 暂存文件路径
// @return stage 的错误;服务已停止时为 ErrStopped
func (s *Service) Restore(name string) (string, error) {
	var (
		path string
		err  error
	)
	if serr := s.submit(func(ctx context.Context) { path, err = s.stage(ctx, name) }); serr != nil {
		return "", serr
	}
	return path, err
}

// submit 把一项手动操作交给 Run 并等它结束。
// @param run 操作本体
// @return 服务已停止、操作没有执行时为 ErrStopped
func (s *Service) submit(run func(ctx context.Context)) error {
	j := job{run: run, done: make(chan struct{})}
	select {
	case s.jobs <- j:
	case <-s.stopped:
		return ErrStopped
	}
	<-j.done
	return nil
}

// serveJobs 逐项执行手动操作,直至 ctx 取消;与定时备份经 RunOnce 的锁串行。
// @param ctx 服务的生命周期上下文
func (s *Service) serveJobs(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-s.jobs:
			j.run(ctx)
			close(j.done)
		}
	}
}
