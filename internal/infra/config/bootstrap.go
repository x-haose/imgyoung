package config

import (
	"fmt"
	"net/netip"
	"os"
	"strings"
)

// envPrefix 引导配置环境变量的统一前缀。
const envPrefix = "IMGYOUNG_"

// 引导配置的环境变量名。
const (
	envListen         = envPrefix + "LISTEN"          // HTTP 监听地址
	envDataDir        = envPrefix + "DATA_DIR"        // 数据目录
	envMasterKey      = envPrefix + "MASTER_KEY"      // 主密钥(64 位 hex)
	envTrustedProxies = envPrefix + "TRUSTED_PROXIES" // 信任其转发头的反向代理网段,逗号分隔
	envMetricsAllow   = envPrefix + "METRICS_ALLOW"   // 允许抓取 /metrics 的网段,逗号分隔
)

// 引导配置默认值。
const (
	defaultListen         = ":8721"               // 默认监听地址,避开 8080/8000/8888 等易冲突端口
	defaultDataDir        = "data"                // 默认数据目录(相对工作目录)
	defaultTrustedProxies = "127.0.0.0/8,::1/128" // 默认只信同机反代:回环对端伪造转发头等于本机进程作恶,已在信任边界内
	// 默认只让内网抓指标:指标暴露路由模板、渠道名与用量,不该对公网可见。
	defaultMetricsAllow = "127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7"
)

// Bootstrap 引导配置:进程活起来之前就得知道的配置,只认环境变量。
// 改动它们需要重启;其余运营配置一律走 settings 表(见 Store),后台修改热生效。
type Bootstrap struct {
	Listen         string         // HTTP 监听地址
	DataDir        string         // 数据目录(SQLite、本地渠道与主密钥文件)
	MasterKey      string         // 主密钥(64 位 hex);空则用数据目录下的 master.key 文件
	TrustedProxies []netip.Prefix // 连接对端落在这些网段时才采信 X-Forwarded-For / X-Forwarded-Proto / X-Forwarded-Host
	MetricsAllow   []netip.Prefix // 客户端地址落在这些网段时才放行 /metrics
}

// LoadBootstrap 从环境变量装配引导配置;空值取默认。
// @Description: 网段清单在此解析,写错一项即启动失败并指出变量名与条目;监听地址与密钥
// 格式的合法性由绑定端口 / 解析密钥的使用点快速失败,错误信息更贴近现场。
// @return 引导配置
// @return 网段清单无法解析时的错误
func LoadBootstrap() (Bootstrap, error) {
	trusted, err := ParsePrefixes(envOrDefault(envTrustedProxies, defaultTrustedProxies))
	if err != nil {
		return Bootstrap{}, fmt.Errorf("%s: %w", envTrustedProxies, err)
	}
	metrics, err := ParsePrefixes(envOrDefault(envMetricsAllow, defaultMetricsAllow))
	if err != nil {
		return Bootstrap{}, fmt.Errorf("%s: %w", envMetricsAllow, err)
	}
	return Bootstrap{
		Listen:         envOrDefault(envListen, defaultListen),
		DataDir:        envOrDefault(envDataDir, defaultDataDir),
		MasterKey:      os.Getenv(envMasterKey),
		TrustedProxies: trusted,
		MetricsAllow:   metrics,
	}, nil
}

// ParsePrefixes 解析逗号分隔的网段清单;单个地址视为仅含该地址的网段,空项忽略。
// @param raw 形如 "127.0.0.0/8, ::1, 10.0.0.0/8" 的文本
// @return 规范化(主机位清零)的网段列表
// @return 任一项既不是 CIDR 也不是 IP 地址时的错误
func ParsePrefixes(raw string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for item := range strings.SplitSeq(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if p, err := netip.ParsePrefix(item); err == nil {
			out = append(out, p.Masked())
			continue
		}
		addr, err := netip.ParseAddr(item)
		if err != nil {
			return nil, fmt.Errorf("网段 %q 既不是 CIDR 也不是 IP 地址", item)
		}
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return out, nil
}

// FormatPrefixes 把网段列表还原为逗号分隔文本,供只读展示。
// @param prefixes 网段列表
// @return 逗号加空格分隔的文本
func FormatPrefixes(prefixes []netip.Prefix) string {
	items := make([]string, len(prefixes))
	for i, p := range prefixes {
		items[i] = p.String()
	}
	return strings.Join(items, ", ")
}

// envOrDefault 读取环境变量,空(未设置或置空)时返回默认值。
// @param key 环境变量名
// @param def 默认值
// @return 生效值
func envOrDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
