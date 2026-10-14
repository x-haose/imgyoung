package alert

import (
	"context"
	"fmt"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// conditions 汇总本轮成立的全部告警条件。
// @param ctx 控制超时与取消
// @return 条件列表,可为空
// @return 读取现状失败时的错误
func (s *Service) conditions(ctx context.Context) ([]Condition, error) {
	channels, err := s.cfg.Queries.ListChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取渠道列表: %w", err)
	}
	nameByID := make(map[int64]string, len(channels))
	for _, c := range channels {
		nameByID[c.ID] = c.Name
	}
	out := channelConditions(channels)
	replicas, err := s.replicaConditions(ctx, nameByID)
	if err != nil {
		return nil, err
	}
	out = append(out, replicas...)
	quota, err := s.quotaConditions(ctx)
	if err != nil {
		return nil, err
	}
	out = append(out, quota...)
	if s.cfg.Backup != nil {
		if message, failed := s.cfg.Backup.LastFailure(); failed {
			out = append(out, Condition{Kind: KindBackupFailed, Message: message})
		}
	}
	return out, nil
}

// channelConditions 从渠道表判定不健康与排空失败。
// @Description: 清理中的渠道不参与选路与探测,其 last_error 只会来自排空失败;在役渠道不健康时
// 以最近探测的失败原因为描述,尚未探测过的以通用描述。
// @param channels 渠道表全部行
// @return 条件列表
func channelConditions(channels []sqlc.Channel) []Condition {
	var out []Condition
	for _, c := range channels {
		reason := c.LastError.String
		if c.PendingDelete != 0 {
			if reason != "" {
				out = append(out, Condition{Kind: KindDrainFailed, Target: c.Name, Message: reason})
			}
			continue
		}
		if c.Healthy != 0 {
			continue
		}
		if reason == "" {
			reason = "渠道探测不健康"
		}
		out = append(out, Condition{Kind: KindChannelUnhealthy, Target: c.Name, Message: reason})
	}
	return out
}

// replicaConditions 从副本水位判定补齐失败:渠道上有 failed 副本即告警,计数进描述。
// @param ctx 控制超时与取消
// @param nameByID 渠道主键到名字的映射;表里已没有的渠道其副本行随之消失,不会出现在水位里
// @return 条件列表
// @return 读取水位失败时的错误
func (s *Service) replicaConditions(ctx context.Context, nameByID map[int64]string) ([]Condition, error) {
	stats, err := s.cfg.Queries.ChannelReplicaStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取副本水位: %w", err)
	}
	var out []Condition
	for _, st := range stats {
		name, ok := nameByID[st.ChannelID]
		if !ok || st.Failed == 0 {
			continue
		}
		out = append(out, Condition{
			Kind: KindReplicationFailed, Target: name,
			Message: fmt.Sprintf("%d 份副本补齐失败,渠道恢复健康后会重试", st.Failed),
		})
	}
	return out, nil
}

// quotaConditions 从渠道用量判定超配额;阈值 <= 0 时不判定。
// @param ctx 控制超时与取消
// @return 条件列表
// @return 读取用量失败时的错误
func (s *Service) quotaConditions(ctx context.Context) ([]Condition, error) {
	quota := s.cfg.QuotaBytes()
	if quota <= 0 {
		return nil, nil
	}
	rows, err := s.cfg.Queries.ChannelStorageUsage(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取渠道用量: %w", err)
	}
	var out []Condition
	for _, row := range rows {
		if row.Bytes > quota {
			out = append(out, Condition{
				Kind: KindChannelQuotaExceeded, Target: row.Name,
				Message: fmt.Sprintf("用量 %d 字节已超过阈值 %d 字节,留意账单与容量", row.Bytes, quota),
			})
		}
	}
	return out, nil
}
