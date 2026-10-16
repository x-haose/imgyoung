package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// 投递的告警状态。
const (
	statusOpen     = "open"     // 告警开启
	statusResolved = "resolved" // 告警恢复
)

// payload Webhook 请求体,一次投递一条告警的一次转折。
type payload struct {
	Kind       string            `json:"kind"`                  // 告警类型
	Target     string            `json:"target"`                // 告警对象
	Message    string            `json:"message"`               // 中文描述
	Params     map[string]string `json:"params"`                // 描述参数,与管理后台告警接口的 params 相同;没有参数时为空对象
	Status     string            `json:"status"`                // open / resolved
	OpenedAt   string            `json:"opened_at"`             // 开启时刻,UTC 文本
	ResolvedAt string            `json:"resolved_at,omitempty"` // 恢复时刻,UTC 文本;开启投递时为空
}

// deliver 把尚未投递的开启与恢复按告警先后逐条 POST 到 Webhook;地址为空时一律记为已投递。
// @Description: 同一对象(类型加对象)的前一条告警这轮没投出去,后面的也不投,留到下一轮按顺序补——否则接收方
// 可能先收到新告警的开启、再收到旧告警的恢复,误以为该对象已经恢复。
// @param ctx 控制超时与取消
// @return 读取或标记告警表失败时的错误
func (s *Service) deliver(ctx context.Context) error {
	pending, err := s.cfg.Queries.ListUnnotifiedAlerts(ctx)
	if err != nil {
		return fmt.Errorf("读取待投递告警: %w", err)
	}
	url := s.cfg.WebhookURL()
	held := map[key]bool{}
	for _, a := range pending {
		k := key{a.Kind, a.Target}
		if held[k] {
			continue
		}
		delivered, err := s.notify(ctx, url, a)
		if err != nil {
			return err
		}
		if !delivered {
			held[k] = true
		}
	}
	return nil
}

// notify 投递一条告警尚未投递的转折:先开启后恢复,接收方看到的顺序才完整。
// @Description: 投递失败只记日志、留待下一轮,不算错误。
// @param ctx 控制超时与取消
// @param url Webhook 地址;空表示不外发、直接记为已投递
// @param a 告警行
// @return delivered 这条告警待投的转折是否都已投出
// @return err 解析参数或标记告警表失败时的错误
func (s *Service) notify(ctx context.Context, url string, a sqlc.Alert) (delivered bool, err error) {
	params, err := DecodeParams(a.Params)
	if err != nil {
		return false, fmt.Errorf("告警 %d: %w", a.ID, err)
	}
	if a.OpenNotified == 0 {
		if url != "" && !s.post(ctx, url, payloadFor(a, params, statusOpen)) {
			return false, nil
		}
		if err := s.cfg.Queries.MarkAlertOpenNotified(ctx, a.ID); err != nil {
			return false, fmt.Errorf("标记告警 %d 开启已投递: %w", a.ID, err)
		}
	}
	if !a.ResolvedAt.Valid || a.ResolveNotified != 0 {
		return true, nil
	}
	if url != "" && !s.post(ctx, url, payloadFor(a, params, statusResolved)) {
		return false, nil
	}
	if err := s.cfg.Queries.MarkAlertResolveNotified(ctx, a.ID); err != nil {
		return false, fmt.Errorf("标记告警 %d 恢复已投递: %w", a.ID, err)
	}
	return true, nil
}

// payloadFor 组装一条告警某次转折的请求体。
// @param a 告警行
// @param params 告警行解出的描述参数
// @param status 本次投递的状态
// @return 请求体
func payloadFor(a sqlc.Alert, params map[string]string, status string) payload {
	return payload{
		Kind: a.Kind, Target: a.Target, Message: a.Message, Params: params, Status: status,
		OpenedAt: a.OpenedAt, ResolvedAt: a.ResolvedAt.String,
	}
}

// post 向 Webhook 投递一条请求体,2xx 视为成功;失败只记日志。
// @param ctx 控制超时与取消
// @param url Webhook 地址
// @param p 请求体
// @return 是否投递成功
func (s *Service) post(ctx context.Context, url string, p payload) bool {
	body, err := json.Marshal(p)
	if err != nil {
		s.cfg.Logger.Error("编码告警请求体失败", "err", err)
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		s.cfg.Logger.Error("构造告警请求失败", "err", err)
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.cfg.Client.Do(req)
	if err != nil {
		s.cfg.Logger.Warn("告警 Webhook 投递失败,下一轮重试", "kind", p.Kind, "target", p.Target, "err", err)
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		s.cfg.Logger.Warn("告警 Webhook 拒收,下一轮重试", "kind", p.Kind, "target", p.Target, "status", resp.StatusCode)
		return false
	}
	return true
}
