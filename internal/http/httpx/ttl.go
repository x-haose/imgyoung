package httpx

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/x-haose/imgyoung/internal/infra/config"
)

// MaxTTLSeconds ttl 秒数上限:config.MaxDurationHours 折算成秒,与管理面 ttl_hours 同口径。
const MaxTTLSeconds = config.MaxDurationHours * 3600

// ParseTTL 解析请求里的 ttl 取值;未显式指定时的"用户默认 > 全局默认"回退
// 由上传服务按用户行解析,本层只翻译请求。
// @Description: ttl 单位秒;"never" 或 0 表示显式永久;空表示未显式指定。
// @param raw ttl 原始取值
// @return 显式指定的有效时长(0 表示显式永久)
// @return 是否显式指定了 ttl
// @return ttl 取值合法为 true;非法(非整数、负数或超上限)为 false
func ParseTTL(raw string) (ttl time.Duration, explicit, ok bool) {
	switch raw {
	case "never":
		return 0, true, true
	case "":
		return 0, false, true
	default:
		secs, err := strconv.Atoi(raw)
		if err != nil || secs < 0 || secs > MaxTTLSeconds {
			return 0, false, false
		}
		return time.Duration(secs) * time.Second, true, true
	}
}

// TTLString 把 JSON 里的 ttl 取值归一成 ParseTTL 接受的字符串。
// @Description: 数字(3600)与字符串("3600" / "never")都收——客户端两种写法都自然,
// 不值得为此逼用户记住某一种。
// @param raw 原始 JSON 片段;为空表示未出现该字段
// @return 归一后的取值,空串表示未指定
func TTLString(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return ""
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return str
	}
	return s
}
