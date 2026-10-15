package failover

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/x-haose/imgyoung/store/driver"
)

// ProbeKey 轻探测查询的约定对象键;从不写入,对它 Stat 得到 driver.ErrNotFound 即说明后端可达。
const ProbeKey = ".imgyoung-health-probe"

// deepProbePrefix 深探测临时对象键的前缀;探后即删,删除失败才会留下残留(见 IsDeepProbeKey)。
const deepProbePrefix = ".imgyoung-probe-"

// IsDeepProbeKey 判断对象键是否为 DeepProbe 写出的临时对象。
// @Description: DeepProbe 删除失败时会留下这类残留;它们只是探测用的小对象,调用方的清理逻辑可据此认领回收。
// @param key 对象键
// @return 是深探测临时对象为 true
func IsDeepProbeKey(key string) bool {
	return strings.HasPrefix(key, deepProbePrefix)
}

// ErrProbeMismatch 深探测读回的内容与写入的不一致:后端在静默改写或截断对象。
var ErrProbeMismatch = errors.New("failover: 探测对象读回内容不一致")

// cleanupTimeout 深探测删除临时对象的超时;独立于调用方的超时,读回超时不连累清理。
const cleanupTimeout = 10 * time.Second

// probePayload 深探测写入的对象内容(固定小对象,读回校验一致性)。
var probePayload = []byte("imgyoung-deep-probe")

// Probe 轻探测:只读查询一个约定不存在的对象,判断后端是否可达。
// @Description: 查询成功或得到 driver.ErrNotFound 都视为可达;零写删成本,适合周期执行。
// 只读成功不代表能写——只读凭证、桶满、配额耗尽都探不出来,需要时用 DeepProbe。
// 超时由 ctx 决定。
// @param d 目标驱动
// @return 后端不可达时的原因错误;nil 表示可达
func Probe(ctx context.Context, d driver.Driver) error {
	_, err := d.Stat(ctx, ProbeKey)
	if err == nil || errors.Is(err, driver.ErrNotFound) {
		return nil
	}
	return err
}

// DeepProbe 深探测:写入一个随机键的小对象、读回核对、再删除。
// @Description: 能暴露"可读不可写"的假健康;随机键使并发探测互不干扰。写入与读回受 ctx 约束,
// 删除改用脱离取消的独立超时,读回失败或 ctx 到期后仍会尽力清理。
// @param d 目标驱动
// @return 任一环节失败时的错误;nil 表示写、读、删全部通过
func DeepProbe(ctx context.Context, d driver.Driver) error {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Errorf("生成探测键: %w", err)
	}
	key := deepProbePrefix + hex.EncodeToString(buf[:])
	if err := d.Put(ctx, key, bytes.NewReader(probePayload), int64(len(probePayload))); err != nil {
		return fmt.Errorf("写入探测对象: %w", err)
	}
	readErr := readBack(ctx, d, key)
	delCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	delErr := d.Delete(delCtx, key)
	if readErr != nil {
		return readErr
	}
	if delErr != nil && !errors.Is(delErr, driver.ErrNotFound) {
		return fmt.Errorf("删除探测对象: %w", delErr)
	}
	return nil
}

// readBack 读回探测对象并核对内容一致。
// @param d 目标驱动
// @param key 探测对象键
// @return 读取失败或内容不一致时的错误
func readBack(ctx context.Context, d driver.Driver, key string) error {
	rc, err := d.Open(ctx, key)
	if err != nil {
		return fmt.Errorf("读回探测对象: %w", err)
	}
	got, err := io.ReadAll(rc)
	if cerr := rc.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("读取探测对象内容: %w", err)
	}
	if !bytes.Equal(got, probePayload) {
		return ErrProbeMismatch
	}
	return nil
}
