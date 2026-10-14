// Package observability 汇聚可观测性设施:Prometheus 指标注册与暴露。
// 只提供与业务无关的采集原语,读取核心状态的闭包由
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
	reg            *prometheus.Registry     // 独立注册表(不用全局默认,避免测试/多实例串扰)
	httpRequests   *prometheus.CounterVec   // 按方法/路由模板/状态码计数的请求总量
	httpDuration   *prometheus.HistogramVec // 按方法/路由模板的请求时延分布
	channelStorage *prometheus.GaugeVec     // 各渠道逻辑存储用量(字节),按渠道名
	uploadDuration *prometheus.HistogramVec // 上传管道耗时分布,按结果
	channelPuts    *prometheus.CounterVec   // 向渠道写对象的次数,按渠道名与成败
	variantLookups *prometheus.CounterVec   // 衍生图缓存查找次数,按命中与否
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
	channelStorage := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace, Subsystem: "channel", Name: "storage_bytes",
		Help: "各渠道逻辑存储用量(字节,按已就绪副本的图片大小求和)。",
	}, []string{"channel"})
	uploadDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace, Subsystem: "upload", Name: "duration_seconds",
		Help:    "上传管道耗时(秒),按结果:ok / rejected(输入或配额不合规)/ failed(基础设施失败)。",
		Buckets: prometheus.DefBuckets,
	}, []string{"outcome"})
	channelPuts := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Subsystem: "channel", Name: "puts_total",
		Help: "向渠道写对象的次数(同步写入与异步补齐),按渠道名与成败。",
	}, []string{"channel", "result"})
	variantLookups := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Subsystem: "variant", Name: "lookups_total",
		Help: "衍生图缓存查找次数,按命中与否。",
	}, []string{"result"})
	reg.MustRegister(httpRequests, httpDuration, channelStorage, uploadDuration, channelPuts, variantLookups)

	return &Metrics{
		reg: reg, httpRequests: httpRequests, httpDuration: httpDuration, channelStorage: channelStorage,
		uploadDuration: uploadDuration, channelPuts: channelPuts, variantLookups: variantLookups,
	}
}

// ObserveUpload 记录一次上传管道的耗时与结果。
// @param outcome 结果:ok / rejected / failed
// @param dur 管道耗时
func (m *Metrics) ObserveUpload(outcome string, dur time.Duration) {
	m.uploadDuration.WithLabelValues(outcome).Observe(dur.Seconds())
}

// RecordChannelPut 记录一次向渠道写对象的结果。
// @param channel 渠道名
// @param ok 是否成功
func (m *Metrics) RecordChannelPut(channel string, ok bool) {
	m.channelPuts.WithLabelValues(channel, resultLabel(ok)).Inc()
}

// RecordVariantLookup 记录一次衍生图缓存查找。
// @param hit 是否命中
func (m *Metrics) RecordVariantLookup(hit bool) {
	result := "miss"
	if hit {
		result = "hit"
	}
	m.variantLookups.WithLabelValues(result).Inc()
}

// resultLabel 把成败翻译成标签值。
// @param ok 是否成功
// @return ok / error
func resultLabel(ok bool) string {
	if ok {
		return "ok"
	}
	return "error"
}

// SetChannelStorage 设置某渠道的存储用量量表值(由用量上报器周期调用)。
// @param channel 渠道名
// @param bytes 逻辑存储字节数
func (m *Metrics) SetChannelStorage(channel string, bytes float64) {
	m.channelStorage.WithLabelValues(channel).Set(bytes)
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
