package upload

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/id"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
)

// ErrKeyMismatch 对象内容的哈希与对象键里的哈希前缀不符:对象损坏,或不是这个键对应的内容。
var ErrKeyMismatch = errors.New("upload: 对象内容与键名里的哈希不符")

// Description 一份已存储字节的格式、尺寸与指纹。
type Description struct {
	MIME   string // 真实 MIME,与解码格式交叉验证过
	Width  int64  // 像素宽
	Height int64  // 像素高
	SHA256 string // 字节的 SHA-256 十六进制
	PHash  int64  // 感知哈希,按位存入 int64
}

// Describe 判定已存储字节的格式、读出尺寸并计算指纹;字节原样使用,不再净化。
// @Description: 与上传管道同一套格式判定(白名单、嗅探与解码格式交叉验证)和像素预算;边长与宽高比是上传时的
// 准入规则,渠道上已有的对象不再按它拒收。
// @param data 对象的完整字节
// @param set 配置快照,取其中的像素预算
// @return 格式、尺寸与指纹
// @return ErrUnsupportedType / ErrInvalidImage / ErrTooManyPixels
func Describe(data []byte, set config.Settings) (Description, error) {
	meta, err := sniff(data, set.MaxPixels())
	if err != nil {
		return Description{}, err
	}
	prep, err := prepare(data, meta, imaging.PolicyPassthrough, set)
	if err != nil {
		return Description{}, err
	}
	return Description{
		MIME: meta.mime, Width: int64(meta.width), Height: int64(meta.height), SHA256: prep.sha256, PHash: prep.phash,
	}, nil
}

// AdoptParams 为渠道上已有的对象补登记图片记录所需的输入。
type AdoptParams struct {
	Key     string  // 对象键,须符合 id.IsStorageKey;其中的短 ID 即登记出的图片短 ID
	Data    []byte  // 对象的完整字节
	UserID  string  // 归属用户 id,须是已存在的用户
	Holders []int64 // 持有该对象的渠道 ID,至少一个
}

// Adopt 为渠道上已有、库里没有记录的对象补登记图片:一个事务写入 blob、引用与各渠道的副本状态。
// @Description: 登记出的图片永不过期,没有文件名与来源 Token;原链接(对象键里的短 ID)随之恢复。
// 持有对象的渠道记为就绪,其余在役渠道记为待补齐并入队。登记在持有渠道的写入租约内进行,与排空渠道串行:
// 租约取不到(渠道已标为清理中)的渠道不算持有者。
// @param ctx 控制超时与取消
// @param p 对象键、字节、归属用户与持有它的渠道
// @return 登记出的图片短 ID
// @return ErrKeyMismatch、Describe 的错误;没有可用的持有渠道时为 ErrInsufficientReplicas;落库失败(如短 ID
// 已被占用、用户不存在)时的错误
func (s *Service) Adopt(ctx context.Context, p AdoptParams) (string, error) {
	if !id.IsStorageKey(p.Key) {
		return "", fmt.Errorf("upload: 非法的对象键 %q", p.Key)
	}
	desc, err := Describe(p.Data, s.cfg.Settings())
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(desc.SHA256, p.Key[id.Length+1:]) {
		return "", fmt.Errorf("%w: %s", ErrKeyMismatch, p.Key)
	}
	held, release := s.holdChannels(p.Holders)
	defer release()
	if len(held) == 0 {
		return "", fmt.Errorf("%w: 持有对象 %s 的渠道都已不在役", ErrInsufficientReplicas, p.Key)
	}
	shortID := p.Key[:id.Length]
	blobID, err := s.persist(ctx, persistParams{
		shortID: shortID, userID: p.UserID, doneIDs: held, channels: s.cfg.Channels.All(), need: 1,
		sha256: desc.SHA256, phash: desc.PHash, key: p.Key, mime: desc.MIME, size: int64(len(p.Data)),
		width: desc.Width, height: desc.Height,
	})
	if err != nil {
		return "", fmt.Errorf("登记对象 %s: %w", p.Key, err)
	}
	s.enqueuePending(ctx, blobID, shortID, s.cfg.Channels.All(), held)
	return shortID, nil
}

// holdChannels 取指定渠道的写入租约;取不到的(已标为清理中或已删除)不返回。
// @param ids 渠道 ID
// @return 取得租约的渠道 ID,按优先级降序
// @return 释放全部租约
func (s *Service) holdChannels(ids []int64) (held []int64, release func()) {
	var releases []func()
	for _, h := range s.cfg.Channels.Healthy() {
		if !slices.Contains(ids, h.ID) {
			continue
		}
		if rel, ok := s.cfg.Channels.HoldWrites(h.ID); ok {
			held = append(held, h.ID)
			releases = append(releases, rel)
		}
	}
	return held, func() {
		for _, rel := range releases {
			rel()
		}
	}
}
