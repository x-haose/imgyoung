package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/infra/db"
)

// channelBodyLimit 渠道管理请求体上限。
const channelBodyLimit = 1 << 20

// ChannelAdmin 渠道管理操作(由 channel.Manager 实现)。
type ChannelAdmin interface {
	// Create 创建渠道并重载。
	Create(ctx context.Context, p channel.CreateParams) (int64, error)
	// Delete 删除渠道(持有唯一副本时拒绝)。
	Delete(ctx context.Context, channelID int64) error
	// List 返回全部渠道描述(不含配置与凭证)。
	List(ctx context.Context) ([]channel.Info, error)
}

// channelCreateRequest 创建渠道的请求体。
type channelCreateRequest struct {
	Name            string            `json:"name"`              // 渠道名(唯一)
	Driver          string            `json:"driver"`            // rclone 后端名
	Priority        int64             `json:"priority"`          // 优先级,越大越优先
	Encrypted       *bool             `json:"encrypted"`         // 是否加密渠道;缺省用全局默认(创建时固化)
	AllowDirectLink bool              `json:"allow_direct_link"` // 是否允许 302 直链
	Config          map[string]string `json:"config"`            // 后端参数(含约定键 path / public_base_url)
}

// handleChannelCreate 创建渠道。
func (s *Server) handleChannelCreate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, channelBodyLimit)
	dec := json.NewDecoder(r.Body)
	// 拒绝未知字段:字段名拼错静默取零值是无声的配置事故。
	dec.DisallowUnknownFields()
	var req channelCreateRequest
	if err := dec.Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "请求体不是合法 JSON 或含未知字段")
		return
	}
	encrypted := s.cfg.EncryptNewChannels
	if req.Encrypted != nil {
		encrypted = *req.Encrypted
	}
	channelID, err := s.cfg.Admin.Create(r.Context(), channel.CreateParams{
		Name:            req.Name,
		Driver:          req.Driver,
		Config:          req.Config,
		Priority:        req.Priority,
		Encrypted:       encrypted,
		AllowDirectLink: req.AllowDirectLink,
	})
	switch {
	case errors.Is(err, channel.ErrInvalidChannel):
		s.writeError(w, http.StatusBadRequest, "渠道参数非法或后端无法初始化")
		return
	// SQLite 无类型化的唯一约束错误,按消息识别。
	case err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed"):
		s.writeError(w, http.StatusConflict, "渠道名已存在")
		return
	case err != nil:
		s.cfg.Logger.Error("创建渠道失败", "name", req.Name, "err", err)
		s.writeError(w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	s.cfg.Logger.Info("渠道已创建", "id", channelID, "name", req.Name, "encrypted", encrypted)
	s.recordAudit(r.Context(), audit.ActionChannelCreate, req.Name,
		fmt.Sprintf("id=%d encrypted=%v", channelID, encrypted))
	s.writeJSON(w, http.StatusCreated, map[string]int64{"id": channelID})
}

// handleChannelList 列出全部渠道(不含配置与凭证)。
func (s *Server) handleChannelList(w http.ResponseWriter, r *http.Request) {
	list, err := s.cfg.Admin.List(r.Context())
	if err != nil {
		s.cfg.Logger.Error("查询渠道列表失败", "err", err)
		s.writeError(w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	s.writeJSON(w, http.StatusOK, list)
}

// handleChannelDelete 删除渠道;持有唯一副本时返回 409。
func (s *Server) handleChannelDelete(w http.ResponseWriter, r *http.Request) {
	channelID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "渠道 ID 非法")
		return
	}
	err = s.cfg.Admin.Delete(r.Context(), channelID)
	switch {
	case errors.Is(err, channel.ErrHoldsSoleReplicas):
		s.writeError(w, http.StatusConflict, "该渠道持有部分图片的唯一副本,删除会丢数据;请先迁移或删除相关图片")
		return
	case err != nil:
		s.cfg.Logger.Error("删除渠道失败", "id", channelID, "err", err)
		s.writeError(w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	s.cfg.Logger.Info("渠道已删除", "id", channelID)
	s.recordAudit(r.Context(), audit.ActionChannelDelete, strconv.FormatInt(channelID, 10), "")
	w.WriteHeader(http.StatusNoContent)
}

// recordAudit best-effort 记录管理操作;审计写入失败只告警,不阻断主操作
// (留痕缺失优于因日志失败而拒绝合法管理操作)。单用户期操作者恒为默认用户。
// @param ctx 请求上下文
// @param action 动作标识(见 audit 包常量)
// @param target 操作目标标识
// @param detail 附加说明
func (s *Server) recordAudit(ctx context.Context, action, target, detail string) {
	if err := s.cfg.Audit.Record(ctx, db.DefaultUserID, action, target, detail); err != nil {
		s.cfg.Logger.Warn("审计记录失败", "action", action, "target", target, "err", err)
	}
}
