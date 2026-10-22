package importer

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// PageFailure 一个没有抓取成功的页面。
type PageFailure struct {
	Source string `json:"source"` // 页面地址
	Code   string `json:"code"`   // 原因码,取值同上传与 URL 转存的错误码,站点地图或订阅源无法解析时为 import_listing_invalid
	Detail string `json:"detail"` // 根因原文,最长 maxDetailRunes 个字符
}

// Unfetchable 一处服务端无法抓取的图片引用。
type Unfetchable struct {
	Source    string `json:"source"`    // 引用所在的页面地址或文档名
	Reference string `json:"reference"` // 引用原文,最长 200 个字符
}

// maxReportEntries 扫描报告里逐条列出的失败页面与无法抓取的引用各保留的条数上限;超出的只计入总数。
// 报告整份存进任务行并随每次读取任务详情返回,大站点大面积失败时不能让它无限增长。
const maxReportEntries = 1000

// ScanReport 扫描结束时的汇总,以 JSON 存在 import_jobs.scan_report。
type ScanReport struct {
	Pages              int64         `json:"pages"`                // 扫描的页面或文档数
	PagesWithoutImages int64         `json:"pages_without_images"` // 其中没有找到任何图片引用的数量
	OffsitePages       int64         `json:"offsite_pages"`        // 站点地图或订阅源里不在其所属域名下而被跳过的数量
	FailedPages        []PageFailure `json:"failed_pages"`         // 没有抓取成功的页面,同一个地址只列一次,至多 maxReportEntries 条
	FailedPagesTotal   int64         `json:"failed_pages_total"`   // 没有抓取成功的页面总数
	Unfetchable        []Unfetchable `json:"unfetchable"`          // 服务端无法抓取的图片引用,至多 maxReportEntries 条
	UnfetchableTotal   int64         `json:"unfetchable_total"`    // 服务端无法抓取的图片引用总数
}

// addFailure 记一个失败页面;列表满了只计数。
// @param f 失败页面
func (r *ScanReport) addFailure(f PageFailure) {
	r.FailedPagesTotal++
	if len(r.FailedPages) < maxReportEntries {
		r.FailedPages = append(r.FailedPages, f)
	}
}

// addUnfetchable 记一处无法抓取的引用;列表满了只计数。
// @param u 无法抓取的引用
func (r *ScanReport) addUnfetchable(u Unfetchable) {
	r.UnfetchableTotal++
	if len(r.Unfetchable) < maxReportEntries {
		r.Unfetchable = append(r.Unfetchable, u)
	}
}

// DecodeReport 解出任务行里的扫描报告。
// @param raw scan_report 列;NULL 表示没有扫描过程或尚未扫描完
// @return 扫描报告,数组字段恒非 nil;列为 NULL 时返回 nil
// @return 内容不是合法 JSON 时的错误
func DecodeReport(raw sql.NullString) (*ScanReport, error) {
	if !raw.Valid {
		return nil, nil
	}
	var r ScanReport
	if err := json.Unmarshal([]byte(raw.String), &r); err != nil {
		return nil, fmt.Errorf("解析扫描报告: %w", err)
	}
	if r.FailedPages == nil {
		r.FailedPages = []PageFailure{}
	}
	if r.Unfetchable == nil {
		r.Unfetchable = []Unfetchable{}
	}
	return &r, nil
}

// DecodeSeeds 解出任务行里的起始地址。
// @param raw seeds 列,JSON 字符串数组
// @return 起始地址,恒非 nil
// @return 内容不是合法 JSON 时的错误
func DecodeSeeds(raw string) ([]string, error) {
	seeds := []string{}
	if err := json.Unmarshal([]byte(raw), &seeds); err != nil {
		return nil, fmt.Errorf("解析起始地址: %w", err)
	}
	return seeds, nil
}
