package alert

import (
	"context"
	"fmt"
	"maps"
	"strconv"

	"github.com/x-haose/imgyoung/internal/core/channel"
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
		if e := s.cfg.Backup.LastFailure(); e != nil {
			params := maps.Clone(e.Params)
			if params == nil {
				params = map[string]string{}
			}
			params["code"] = e.Code.String()
			out = append(out, Condition{Kind: KindBackupFailed, Message: e.Error(), Params: params})
		}
	}
	return out, nil
}

// channelConditions 从渠道表判定不健康与排空失败。
// @Description: 清理中的渠道不参与选路与探测,其失败栏只会来自排空失败;在役渠道不健康时以最近一次
// 失败的结论为描述,参数为 stage / reason / detail(见 channel.LastFailure);尚未探测过的没有参数。
// @param channels 渠道表全部行
// @return 条件列表
func channelConditions(channels []sqlc.Channel) []Condition {
	var out []Condition
	for _, c := range channels {
		f := channel.FailureOf(c)
		if c.PendingDelete != 0 {
			if f != nil {
				out = append(out, failureCondition(KindDrainFailed, c.Name, *f))
			}
			continue
		}
		if c.Healthy != 0 {
			continue
		}
		if f == nil {
			out = append(out, Condition{Kind: KindChannelUnhealthy, Target: c.Name, Message: "渠道探测不健康"})
			continue
		}
		out = append(out, failureCondition(KindChannelUnhealthy, c.Name, *f))
	}
	return out
}

// failureCondition 以渠道失败结论构造告警条件。
// @param kind 告警类型
// @param target 渠道名
// @param f 失败结论
// @return 条件
func failureCondition(kind, target string, f channel.LastFailure) Condition {
	return Condition{
		Kind: kind, Target: target, Message: f.Message(),
		Params: map[string]string{"stage": string(f.Stage), "reason": string(f.Reason), "detail": f.Detail},
	}
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
			Params:  map[string]string{"failed": strconv.FormatInt(st.Failed, 10)},
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
				Params: map[string]string{
					"bytes": strconv.FormatInt(row.Bytes, 10), "quota": strconv.FormatInt(quota, 10),
				},
			})
		}
	}
	return out, nil
}
