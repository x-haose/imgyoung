// Package cdn 实现 CDN 边缘缓存清理(一期 Cloudflare):按 URL 批量 purge,
// 供删除级联的异步任务调用。纯标准库 HTTP,不引入厂商 SDK。
package cdn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// purgeBatchSize 单次 purge API 调用的 URL 上限:Cloudflare 按 URL 清除时每个请求最多 100 个(企业版 500),
// 取各套餐通用的 100。
const purgeBatchSize = 100

// requestTimeout 单次 API 调用超时;失败由上层任务队列按退避重试,此处不重试。
const requestTimeout = 30 * time.Second

// maxErrorBody 错误响应体的读取上限,防对端异常大响应占内存。
const maxErrorBody = 4 << 10

// defaultAPIBase Cloudflare API 根地址。
const defaultAPIBase = "https://api.cloudflare.com/client/v4"

// CloudflarePurger 按 URL 清理 Cloudflare 边缘缓存的执行器,无可变状态、并发安全。
type CloudflarePurger struct {
	zoneID  string       // Cloudflare Zone ID
	token   string       // API Token,须具备 Zone.Cache Purge 权限;绝不写入日志与错误消息
	apiBase string       // API 根地址;生产为 Cloudflare 官方地址,集成测试指向本地 HTTP 服务
	client  *http.Client // 带超时的 HTTP 客户端
}

// NewCloudflare 构造指向 Cloudflare 官方 API 的 purge 执行器。
// @param zoneID Cloudflare Zone ID
// @param token API Token(敏感,仅存内存)
// @return 就绪的执行器
func NewCloudflare(zoneID, token string) *CloudflarePurger {
	return NewCloudflareAt(defaultAPIBase, zoneID, token)
}

// NewCloudflareAt 构造指向指定 API 根地址的 purge 执行器,集成测试据此把外呼指向本地 HTTP 服务。
// @param apiBase API 根地址,不含尾斜杠
// @param zoneID Cloudflare Zone ID
// @param token API Token(敏感,仅存内存)
// @return 就绪的执行器
func NewCloudflareAt(apiBase, zoneID, token string) *CloudflarePurger {
	return &CloudflarePurger{
		zoneID:  zoneID,
		token:   token,
		apiBase: apiBase,
		client:  &http.Client{Timeout: requestTimeout},
	}
}

// purgeRequest purge_cache API 请求体。
type purgeRequest struct {
	Files []string `json:"files"` // 待清理的绝对 URL,单批 ≤ purgeBatchSize
}

// purgeResponse purge_cache API 响应信封(只取判定成败所需字段)。
type purgeResponse struct {
	Success *bool `json:"success"` // 调用是否成功;nil 表示响应缺该字段(非标准信封),以 HTTP 状态码判定
	Errors  []struct {
		Code    int    `json:"code"`    // Cloudflare 错误码
		Message string `json:"message"` // 错误描述
	} `json:"errors"` // 失败明细
}

// Purge 按批清理给定 URL 的边缘缓存(每批至多 purgeBatchSize 个,顺序发起)。
// @Description: 任一批失败即返回错误,调用方(任务队列)整体重试——purge 幂等,重复清理无害。
// @param ctx 控制超时与取消
// @param urls 待清理的绝对 URL 列表,空列表直接返回
// @return 网络失败或 API 报错时的错误(不含 Token)
func (p *CloudflarePurger) Purge(ctx context.Context, urls []string) error {
	for start := 0; start < len(urls); start += purgeBatchSize {
		end := min(start+purgeBatchSize, len(urls))
		if err := p.purgeBatch(ctx, urls[start:end]); err != nil {
			return err
		}
	}
	return nil
}

// purgeBatch 发起一次 purge API 调用。
// @param urls 单批 URL(≤ purgeBatchSize)
// @return 网络失败、非 2xx 或信封显式 success=false 时的错误(success 缺失的 2xx 视为成功)
func (p *CloudflarePurger) purgeBatch(ctx context.Context, urls []string) error {
	body, err := json.Marshal(purgeRequest{Files: urls})
	if err != nil {
		return fmt.Errorf("编码 purge 请求: %w", err)
	}
	endpoint := p.apiBase + "/zones/" + p.zoneID + "/purge_cache"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("构造 purge 请求: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("调用 Cloudflare purge API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	if err != nil {
		return fmt.Errorf("读取 purge 响应: %w", err)
	}
	var envelope purgeResponse
	if uerr := json.Unmarshal(payload, &envelope); uerr == nil && envelope.Success != nil && !*envelope.Success {
		if len(envelope.Errors) > 0 {
			e := envelope.Errors[0]
			return fmt.Errorf("cloudflare purge 失败(HTTP %d, 错误 %d): %s", resp.StatusCode, e.Code, e.Message)
		}
		return fmt.Errorf("cloudflare purge 失败(HTTP %d): 信封 success=false 且无错误明细", resp.StatusCode)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("cloudflare purge 失败(HTTP %d)", resp.StatusCode)
	}
	return nil
}
