// Package observability 汇聚可观测性设施:Prometheus 指标注册与暴露
// (设计定稿·可观测性)。只提供与业务无关的采集原语,读取核心状态的闭包由
// 装配根注入,故本包不依赖 core。
package observability

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// namespace 全部指标的统一前缀。
const namespace = "imgyoung"

// Metrics 持有独立 Prometheus 注册表与内置指标;自身并发安全。
type Metrics struct {
	// reg 独立注册表(不用全局默认,避免测试/多实例串扰)。
	reg *prometheus.Registry
	// httpRequests 按方法/路由模板/状态码计数的请求总量。
	httpRequests *prometheus.CounterVec
	// httpDuration 按方法/路由模板的请求时延分布。
	httpDuration *prometheus.HistogramVec
}

// New 创建指标集,注册 Go 运行时与进程采集器及内置 HTTP 指标。
// @return 就绪的指标集
func New() *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	httpRequests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Subsystem: "http", Name: "requests_total",
		Help: "HTTP 请求总数,按方法、路由模板、状态码。",
	}, []string{"method", "route", "status"})
	httpDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace, Subsystem: "http", Name: "request_duration_seconds",
		Help: "HTTP 请求处理时延(秒),按方法、路由模板。", Buckets: prometheus.DefBuckets,
	}, []string{"method", "route"})
	reg.MustRegister(httpRequests, httpDuration)

	return &Metrics{reg: reg, httpRequests: httpRequests, httpDuration: httpDuration}
}

// RecordRequest 记录一次 HTTP 请求的计数与时延。
// @param method 请求方法
// @param route 路由模板(非原始路径,避免高基数)
// @param status 状态码文本
// @param dur 处理耗时
func (m *Metrics) RecordRequest(method, route, status string, dur time.Duration) {
	m.httpRequests.WithLabelValues(method, route, status).Inc()
	m.httpDuration.WithLabelValues(method, route).Observe(dur.Seconds())
}

// RegisterGauge 挂一个 scrape 时求值的量表,供装配根注入读取核心状态的闭包
// (如渠道健康数、队列深度)——让依赖 core 的代码留在装配根,本包保持无核心依赖。
// @param name 指标名(自动加 imgyoung 前缀)
// @param help 帮助文本
// @param f scrape 时求当前值的回调
func (m *Metrics) RegisterGauge(name, help string, f func() float64) {
	m.reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: namespace, Name: name, Help: help,
	}, f))
}

// Handler 返回 /metrics 的 Prometheus 暴露处理器。
// @return promhttp 处理器
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}
