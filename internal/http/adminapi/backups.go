package adminapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/reliability"
	"github.com/x-haose/imgyoung/internal/http/httpx"
)

// HandleBackupList 列出各健康渠道上快照的并集,最新在前(仅管理员路由)。
// @Description: 没有健康渠道是外部依赖故障,回 503;任一渠道列举失败回 500。
func (h *Handlers) HandleBackupList(w http.ResponseWriter, r *http.Request) {
	snapshots, err := h.cfg.Backups.List(r.Context())
	switch {
	case errors.Is(err, channel.ErrNoHealthyChannel):
		httpx.WriteError(h.cfg.Logger, w, http.StatusServiceUnavailable, "没有健康渠道,无法列举快照")
		return
	case err != nil:
		h.cfg.Logger.Error("列举备份快照失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, listResponse{Items: snapshots, Total: int64(len(snapshots))})
}

// HandleBackupCreate 立即备份一次,写到每个健康渠道并返回新快照(仅管理员路由)。
// @Description: 没有健康渠道可存放快照是外部依赖故障,回 503 供稍后重试;其余失败回 500,
// 含部分渠道未写成——错误里列出失败的渠道与原因,写成的渠道保留该快照。
func (h *Handlers) HandleBackupCreate(w http.ResponseWriter, r *http.Request) {
	snap, err := h.cfg.Backups.RunOnce(r.Context())
	switch {
	case errors.Is(err, channel.ErrNoHealthyChannel):
		httpx.WriteError(h.cfg.Logger, w, http.StatusServiceUnavailable, "没有健康渠道可存放快照")
		return
	case err != nil:
		h.cfg.Logger.Error("手动备份失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "备份失败:"+err.Error())
		return
	}
	h.recordAudit(r.Context(), audit.ActionBackupCreate, snap.Name, "")
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusCreated, snap)
}

// HandleBackupDownload 下载一份快照文件(仅管理员路由)。
func (h *Handlers) HandleBackupDownload(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	rc, size, err := h.cfg.Backups.Open(r.Context(), name)
	if err != nil {
		h.writeSnapshotError(w, name, err)
		return
	}
	defer func() { _ = rc.Close() }()
	hd := w.Header()
	hd.Set("Content-Type", "application/vnd.sqlite3")
	hd.Set("Content-Length", strconv.FormatInt(size, 10))
	hd.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	hd.Set("Cache-Control", "no-store")
	if _, err := io.Copy(w, rc); err != nil {
		h.cfg.Logger.Debug("输出快照中断", "snapshot", name, "err", err)
	}
}

// HandleBackupRestore 用一份快照恢复元数据库:暂存后触发优雅停机,进程被拉起时换入(仅管理员路由)。
// @Description: 响应 202 先于停机发出;没有进程管理器拉起时,暂存文件等到下次手动启动再换入。
func (h *Handlers) HandleBackupRestore(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	path, err := h.cfg.Backups.Stage(r.Context(), name)
	if err != nil {
		h.writeSnapshotError(w, name, err)
		return
	}
	h.recordAudit(r.Context(), audit.ActionBackupRestore, name, "staged="+path)
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusAccepted, map[string]string{
		"snapshot": name, "staged": path, "message": "快照已暂存,服务即将重启并换入;请等待进程被重新拉起",
	})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	h.cfg.Shutdown()
}

// writeSnapshotError 把快照操作的错误翻译为状态码。
// @param w 响应写入器
// @param name 快照名
// @param err 备份服务返回的错误
func (h *Handlers) writeSnapshotError(w http.ResponseWriter, name string, err error) {
	switch {
	case errors.Is(err, reliability.ErrSnapshotName):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "快照名非法")
	case errors.Is(err, reliability.ErrSnapshotNotFound):
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, "快照不存在")
	case errors.Is(err, channel.ErrNoHealthyChannel):
		httpx.WriteError(h.cfg.Logger, w, http.StatusServiceUnavailable, "没有健康渠道,无法读取快照")
	default:
		h.cfg.Logger.Error("快照操作失败", "snapshot", name, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
	}
}
