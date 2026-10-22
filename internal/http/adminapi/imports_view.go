package adminapi

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/core/importer"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
)

// importCounts 任务各状态的条目数。
type importCounts struct {
	Candidate int64 `json:"candidate"` // 扫描发现,等待确认
	Pending   int64 `json:"pending"`   // 已确认,等待转存
	Done      int64 `json:"done"`      // 已导入
	Failed    int64 `json:"failed"`    // 转存失败
	Skipped   int64 `json:"skipped"`   // 确认时所在域名未被选中
}

// importScan 扫描进度与结果。
type importScan struct {
	PagesFound int64                `json:"pages_found"` // 已发现的页面数
	PagesDone  int64                `json:"pages_done"`  // 已处理的页面数
	Report     *importer.ScanReport `json:"report"`      // 扫描结束后的汇总;未结束为 null
}

// importJobResponse 导入任务详情。
type importJobResponse struct {
	ID         string       `json:"id"`          // 任务 ID
	Mode       string       `json:"mode"`        // 输入类型
	Status     string       `json:"status"`      // 任务状态
	Seeds      []string     `json:"seeds"`       // site 任务的起始地址
	TTLSeconds *int64       `json:"ttl_seconds"` // 创建时指定的有效期秒数;null 表示按默认
	EXIFPolicy string       `json:"exif_policy"` // 创建时指定的 EXIF 档位;空串表示全局默认
	Referer    string       `json:"referer"`     // 创建时指定的 Referer;空串表示取来源页
	Counts     importCounts `json:"counts"`      // 各状态的条目数
	Scan       importScan   `json:"scan"`        // 扫描进度与结果
	ErrorCode  *string      `json:"error_code"`  // aborted 的原因码;其他状态为 null
	CreatedAt  string       `json:"created_at"`  // 创建时刻
	FinishedAt *string      `json:"finished_at"` // 结束时刻;未结束为 null
}

// importJobFor 读取任务并确认归属;失败时已写响应。
// @param userID 操作者用户 id
// @param jobID 任务 ID
// @return 任务行与是否可继续
func (h *Handlers) importJobFor(w http.ResponseWriter, r *http.Request, userID, jobID string) (sqlc.ImportJob, bool) {
	row, err := h.cfg.Queries.GetImportJob(r.Context(), jobID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && row.UserID != userID) {
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, errcode.ImportNotFound)
		return sqlc.ImportJob{}, false
	}
	if err != nil {
		h.cfg.Logger.Error("读取导入任务失败", "job", jobID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return sqlc.ImportJob{}, false
	}
	return row, true
}

// importJobBody 把任务行翻译为响应体。
// @param row 任务行
// @return 响应体
// @return 条目计数、起始地址或扫描报告读取失败的错误
func (h *Handlers) importJobBody(ctx context.Context, row sqlc.ImportJob) (importJobResponse, error) {
	counts, err := h.importCounts(ctx, row.ID)
	if err != nil {
		return importJobResponse{}, err
	}
	seeds, err := importer.DecodeSeeds(row.Seeds)
	if err != nil {
		return importJobResponse{}, err
	}
	report, err := importer.DecodeReport(row.ScanReport)
	if err != nil {
		return importJobResponse{}, err
	}
	resp := importJobResponse{
		ID: row.ID, Mode: row.Mode, Status: row.Status, Seeds: seeds, TTLSeconds: nullableInt(row.TtlSeconds),
		EXIFPolicy: row.ExifPolicy, Referer: row.Referer, Counts: counts,
		Scan:      importScan{PagesFound: row.ScanPagesFound, PagesDone: row.ScanPagesDone, Report: report},
		CreatedAt: row.CreatedAt, FinishedAt: nullableStr(row.FinishedAt),
	}
	if row.ErrorCode != "" {
		resp.ErrorCode = &row.ErrorCode
	}
	return resp, nil
}

// importCounts 统计任务各状态的条目数。
// @param jobID 任务 ID
// @return 各状态的条目数
// @return 查询失败的错误
func (h *Handlers) importCounts(ctx context.Context, jobID string) (importCounts, error) {
	rows, err := h.cfg.Queries.CountImportItemsByState(ctx, jobID)
	if err != nil {
		return importCounts{}, fmt.Errorf("统计导入条目: %w", err)
	}
	var c importCounts
	for _, row := range rows {
		switch row.State {
		case importer.StateCandidate:
			c.Candidate = row.ItemCount
		case importer.StatePending:
			c.Pending = row.ItemCount
		case importer.StateDone:
			c.Done = row.ItemCount
		case importer.StateFailed:
			c.Failed = row.ItemCount
		case importer.StateSkipped:
			c.Skipped = row.ItemCount
		default:
			// 条目状态由 importer 管理,不会出现表外取值。
		}
	}
	return c, nil
}

// writeImportJob 输出任务详情;组装失败记日志、回 500。
// @param status 响应状态码
// @param row 任务行
func (h *Handlers) writeImportJob(w http.ResponseWriter, r *http.Request, status int, row sqlc.ImportJob) {
	body, err := h.importJobBody(r.Context(), row)
	if err != nil {
		h.cfg.Logger.Error("组装导入任务详情失败", "job", row.ID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	httpx.WriteJSON(h.cfg.Logger, w, status, body)
}

// HandleImportList 分页列出当前用户的导入任务,最新的在前。
func (h *Handlers) HandleImportList(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	limit, offset, err := pagination(r)
	if err != nil {
		h.writeQueryError(w, err)
		return
	}
	items, total, err := h.importPage(r.Context(), p.UserID, limit, offset)
	if err != nil {
		h.cfg.Logger.Error("查询导入任务列表失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, listResponse{Items: items, Total: total})
}

// importPage 查一页任务与总数。
// @param userID 任务的归属用户 id
// @return 任务详情,恒非 nil
// @return 总数
// @return 查询失败的错误
func (h *Handlers) importPage(ctx context.Context, userID string, limit, offset int64) ([]importJobResponse,
	int64, error) {
	rows, err := h.cfg.Queries.ListImportJobs(ctx, sqlc.ListImportJobsParams{
		UserID: userID, PageLimit: limit, PageOffset: offset,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("读取任务: %w", err)
	}
	total, err := h.cfg.Queries.CountImportJobs(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("统计任务: %w", err)
	}
	items, err := h.importJobBodies(ctx, rows)
	return items, total, err
}

// importJobBodies 把一页任务行翻译为响应体;结果恒非 nil。
// @param rows 任务行
// @return 响应体
// @return 组装失败的错误
func (h *Handlers) importJobBodies(ctx context.Context, rows []sqlc.ImportJob) ([]importJobResponse, error) {
	out := make([]importJobResponse, 0, len(rows))
	for _, row := range rows {
		body, err := h.importJobBody(ctx, row)
		if err != nil {
			return nil, err
		}
		out = append(out, body)
	}
	return out, nil
}

// HandleImportGet 导入任务详情。
func (h *Handlers) HandleImportGet(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	h.respondImport(w, r, p.UserID, chi.URLParam(r, "id"))
}

// importHost 待确认的条目按域名分组。
type importHost struct {
	Host    string   `json:"host"`    // 域名
	Count   int64    `json:"count"`   // 待确认的条目数
	Samples []string `json:"samples"` // 最多 3 个示例地址
}

// HandleImportHosts 待确认的条目按域名分组,数量多的在前。
func (h *Handlers) HandleImportHosts(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	row, ok := h.importJobFor(w, r, p.UserID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	hosts, err := h.importHosts(r.Context(), row.ID)
	if err != nil {
		h.cfg.Logger.Error("查询导入域名分组失败", "job", row.ID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, hosts)
}

// importHosts 查出任务的域名分组与各组示例。
// @param jobID 任务 ID
// @return 域名分组,恒非 nil
// @return 查询失败的错误
func (h *Handlers) importHosts(ctx context.Context, jobID string) ([]importHost, error) {
	groups, err := h.cfg.Queries.ListImportHosts(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("统计域名分组: %w", err)
	}
	samples, err := h.cfg.Queries.ListImportHostSamples(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("读取域名示例: %w", err)
	}
	byHost := make(map[string][]string, len(groups))
	for _, s := range samples {
		byHost[s.Host] = append(byHost[s.Host], s.Url)
	}
	out := make([]importHost, 0, len(groups))
	for _, g := range groups {
		out = append(out, importHost{Host: g.Host, Count: g.ItemCount, Samples: byHost[g.Host]})
	}
	return out, nil
}

// importItem 导入任务的一个条目。
type importItem struct {
	ID          int64    `json:"id"`           // 条目 ID
	URL         string   `json:"url"`          // 图片的绝对地址
	Host        string   `json:"host"`         // 域名
	State       string   `json:"state"`        // 条目状态
	ImageID     *string  `json:"image_id"`     // 导入成功后的图片短 ID
	ErrorCode   *string  `json:"error_code"`   // failed 的原因码
	ErrorDetail string   `json:"error_detail"` // 失败的根因原文
	Sources     []string `json:"sources"`      // 引用这张图的页面或文档,最多 5 个
	SourceCount int64    `json:"source_count"` // 引用这张图的页面或文档总数
}

// importStates 条目状态的合法取值。
var importStates = map[string]struct{}{
	importer.StateCandidate: {}, importer.StatePending: {}, importer.StateDone: {},
	importer.StateFailed: {}, importer.StateSkipped: {},
}

// HandleImportItems 分页列出任务的条目,可按状态与域名过滤。
func (h *Handlers) HandleImportItems(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	row, ok := h.importJobFor(w, r, p.UserID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	limit, offset, err := pagination(r)
	state := r.URL.Query().Get("state")
	if _, known := importStates[state]; err == nil && state != "" && !known {
		err = invalidParam("state")
	}
	if err != nil {
		h.writeQueryError(w, err)
		return
	}
	host := r.URL.Query().Get("host")
	items, total, err := h.importItems(r.Context(), row.ID, state, host, limit, offset)
	if err != nil {
		h.cfg.Logger.Error("查询导入条目失败", "job", row.ID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, listResponse{Items: items, Total: total})
}

// importItems 查一页条目与过滤后的总数。
// @param jobID 任务 ID
// @param state 状态过滤,空串不限
// @param host 域名过滤,空串不限
// @return 条目,恒非 nil
// @return 过滤后的总数
// @return 查询或解码失败的错误
func (h *Handlers) importItems(ctx context.Context, jobID, state, host string, limit,
	offset int64) ([]importItem, int64, error) {
	rows, err := h.cfg.Queries.ListImportItems(ctx, sqlc.ListImportItemsParams{
		JobID: jobID, State: state, Host: host, PageLimit: limit, PageOffset: offset,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("读取条目: %w", err)
	}
	total, err := h.cfg.Queries.CountImportItems(ctx,
		sqlc.CountImportItemsParams{JobID: jobID, State: state, Host: host})
	if err != nil {
		return nil, 0, fmt.Errorf("统计条目: %w", err)
	}
	items := make([]importItem, 0, len(rows))
	for _, row := range rows {
		sources := []string{}
		if err := json.Unmarshal([]byte(row.Sources), &sources); err != nil {
			return nil, 0, fmt.Errorf("解析条目来源: %w", err)
		}
		it := importItem{
			ID: row.ID, URL: row.Url, Host: row.Host, State: row.State, ImageID: nullableStr(row.ImageID),
			ErrorDetail: row.ErrorDetail, Sources: sources, SourceCount: row.SourceCount,
		}
		if row.ErrorCode != "" {
			it.ErrorCode = &row.ErrorCode
		}
		items = append(items, it)
	}
	return items, total, nil
}

// importReference 对照表里旧图片的一处引用。
type importReference struct {
	Source string `json:"source"` // 引用所在的页面地址或文档名
	Raw    string `json:"raw"`    // 引用在该处的原文
}

// importMappingRow 对照表的一行。
type importMappingRow struct {
	OldURL     string            `json:"old_url"`    // 旧图片的绝对地址
	NewURL     string            `json:"new_url"`    // 新图片的绝对访问地址
	ImageID    string            `json:"image_id"`   // 新图片的短 ID
	References []importReference `json:"references"` // 旧图片被引用的位置
}

// HandleImportMapping 以附件形式下载新旧链接对照表。
func (h *Handlers) HandleImportMapping(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	row, ok := h.importJobFor(w, r, p.UserID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	format := r.URL.Query().Get("format")
	if format != "" && format != "json" && format != "csv" {
		h.writeQueryError(w, invalidParam("format"))
		return
	}
	rows, err := h.importMapping(r, row.ID)
	if err != nil {
		h.cfg.Logger.Error("生成导入对照表失败", "job", row.ID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	if format == "csv" {
		h.writeMappingCSV(w, row.ID, rows)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="imgyoung-import-`+row.ID+`.json"`)
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, rows)
}

// importMapping 查出已导入条目的对照表,新地址按对外站点地址拼成绝对地址。
// @param jobID 任务 ID
// @return 对照表,恒非 nil
// @return 查询失败的错误
func (h *Handlers) importMapping(r *http.Request, jobID string) ([]importMappingRow, error) {
	items, err := h.cfg.Queries.ListImportMappingItems(r.Context(), jobID)
	if err != nil {
		return nil, fmt.Errorf("读取已导入的条目: %w", err)
	}
	refs, err := h.cfg.Queries.ListImportMappingReferences(r.Context(), jobID)
	if err != nil {
		return nil, fmt.Errorf("读取引用位置: %w", err)
	}
	byItem := make(map[int64][]importReference, len(items))
	for _, ref := range refs {
		byItem[ref.ItemID] = append(byItem[ref.ItemID], importReference{Source: ref.Source, Raw: ref.Raw})
	}
	base := h.cfg.Settings.Current().BaseURL
	out := make([]importMappingRow, 0, len(items))
	for _, it := range items {
		refs := byItem[it.ID]
		if refs == nil {
			refs = []importReference{}
		}
		out = append(out, importMappingRow{
			OldURL: it.Url, NewURL: httpx.ImageURL(r, base, it.ImageID.String), ImageID: it.ImageID.String,
			References: refs,
		})
	}
	return out, nil
}

// writeMappingCSV 输出 CSV 对照表:每条引用一行,没有引用的条目也占一行。
// @Description: 以 = + - @ 与制表符、回车开头的单元格前加单引号,避免用电子表格打开时被当成公式。
// @param jobID 任务 ID,用于文件名
// @param rows 对照表
func (h *Handlers) writeMappingCSV(w http.ResponseWriter, jobID string, rows []importMappingRow) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="imgyoung-import-`+jobID+`.csv"`)
	cw := csv.NewWriter(w)
	write := func(rec ...string) bool {
		if err := cw.Write(rec); err != nil {
			h.cfg.Logger.Warn("写入对照表 CSV 失败,客户端可能已断开", "job", jobID, "err", err)
			return false
		}
		return true
	}
	if !write("old_url", "new_url", "image_id", "source", "raw") {
		return
	}
	for _, row := range rows {
		refs := row.References
		if len(refs) == 0 {
			refs = []importReference{{}}
		}
		for _, ref := range refs {
			if !write(csvSafe(row.OldURL), csvSafe(row.NewURL), row.ImageID, csvSafe(ref.Source), csvSafe(ref.Raw)) {
				return
			}
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		h.cfg.Logger.Warn("写入对照表 CSV 失败,客户端可能已断开", "job", jobID, "err", err)
	}
}

// csvSafe 给可能被电子表格当成公式的单元格前加单引号。
// @param cell 单元格内容
// @return 处理后的内容
func csvSafe(cell string) string {
	if cell != "" && strings.ContainsRune("=+-@\t\r", rune(cell[0])) {
		return "'" + cell
	}
	return cell
}
