package adminapi

import (
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"

	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
)

// 列表分页参数边界。
const (
	defaultPageSize = 20  // 未指定 page_size 时的默认值
	maxPageSize     = 100 // page_size 上限,防单请求拉全表
)

// invalidParam 取值非法的查询参数名;列表接口的查询参数省略时取默认,给出的值不合法即回 400。
type invalidParam string

// Error 实现 error。
// @return 错误说明
func (p invalidParam) Error() string { return "查询参数 " + string(p) + " 取值非法" }

// pagination 解析 page / page_size 为 LIMIT / OFFSET。
// @Description: page 省略为 1,page_size 省略为 defaultPageSize;给出时 page 不小于 1,page_size 在 1 到 maxPageSize 之间。
// @param r 请求
// @return limit 与 offset
// @return 取值非法时的 invalidParam
func pagination(r *http.Request) (limit, offset int64, err error) {
	q := r.URL.Query()
	page, err := intParam(q, "page", 1, 1, math.MaxInt32)
	if err != nil {
		return 0, 0, err
	}
	size, err := intParam(q, "page_size", defaultPageSize, 1, maxPageSize)
	if err != nil {
		return 0, 0, err
	}
	return size, (page - 1) * size, nil
}

// intParam 解析整数查询参数。
// @param q 查询参数
// @param name 参数名
// @param def 省略时的取值
// @param lo 允许的最小值
// @param hi 允许的最大值
// @return 参数值
// @return 不是整数或超出 [lo, hi] 时的 invalidParam
func intParam(q url.Values, name string, def, lo, hi int64) (int64, error) {
	raw := q.Get(name)
	if raw == "" {
		return def, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < lo || v > hi {
		return 0, invalidParam(name)
	}
	return v, nil
}

// timeRange 解析 since / until。
// @Description: 省略为空串,表示该端不限;给出时须为 UTC 时间文本 YYYY-MM-DD HH:MM:SS,与库内时间列逐字可比。
// @param q 查询参数
// @return since 与 until 原文
// @return 格式非法时的 invalidParam
func timeRange(q url.Values) (since, until string, err error) {
	for _, name := range []string{"since", "until"} {
		if raw := q.Get(name); raw != "" {
			if _, perr := sqlc.ParseTimeText(raw); perr != nil {
				return "", "", invalidParam(name)
			}
		}
	}
	return q.Get("since"), q.Get("until"), nil
}

// flagParam 解析只有一个取值的开关参数。
// @param q 查询参数
// @param name 参数名
// @param on 表示开启的唯一取值
// @return 省略时为 false,等于 on 时为 true
// @return 其他取值时的 invalidParam
func flagParam(q url.Values, name, on string) (bool, error) {
	switch q.Get(name) {
	case "":
		return false, nil
	case on:
		return true, nil
	}
	return false, invalidParam(name)
}

// writeQueryError 把查询参数的解析错误写成 400 query_param_invalid,参数名放进 name。
// @param err 上述解析函数返回的错误;不是 invalidParam 属装配错误,回 500
func (h *Handlers) writeQueryError(w http.ResponseWriter, err error) {
	var p invalidParam
	if !errors.As(err, &p) {
		h.cfg.Logger.Error("查询参数解析返回了未登记的错误", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.QueryParamInvalid, "name", string(p))
}
