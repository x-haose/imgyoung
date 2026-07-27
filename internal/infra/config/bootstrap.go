package config

import (
	"os"
	"strings"
)

// envPrefix 引导配置环境变量的统一前缀。
const envPrefix = "IMGYOUNG_"

// 引导配置的环境变量名。
const (
	envListen    = envPrefix + "LISTEN"     // HTTP 监听地址
	envDataDir   = envPrefix + "DATA_DIR"   // 数据目录
	envMasterKey = envPrefix + "MASTER_KEY" // 主密钥(64 位 hex)
)

// 引导配置默认值。
const (
	defaultListen  = ":8721" // 默认监听地址,避开 8080/8000/8888 等易冲突端口
	defaultDataDir = "data"  // 默认数据目录(相对工作目录)
)

// Bootstrap 引导配置:进程活起来之前就得知道的配置,只认环境变量。
// 改动它们需要重启;其余运营配置一律走 settings 表(见 Store),后台修改热生效。
type Bootstrap struct {
	Listen    string // HTTP 监听地址
	DataDir   string // 数据目录(SQLite、本地渠道与主密钥文件)
	MasterKey string // 主密钥(64 位 hex);空则用数据目录下的 master.key 文件
}

// LoadBootstrap 从环境变量装配引导配置;空值取默认。
// @Description: 引导项无跨项约束,不设校验——监听地址与密钥格式的合法性
// 由绑定端口 / 解析密钥的使用点快速失败,错误信息更贴近现场。
// @return 引导配置
func LoadBootstrap() Bootstrap {
	return Bootstrap{
		Listen:    envOrDefault(envListen, defaultListen),
		DataDir:   envOrDefault(envDataDir, defaultDataDir),
		MasterKey: os.Getenv(envMasterKey),
	}
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
