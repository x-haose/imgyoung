package recovery_test

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"testing"

	"github.com/x-haose/imgyoung/internal/core/recovery"
	"github.com/x-haose/imgyoung/internal/core/testenv"
	"github.com/x-haose/imgyoung/internal/core/upload"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/id"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
)

// rig 找回测试环境:真实库与 local 渠道之上的上传服务与找回服务。
type rig struct {
	*testenv.Env
	upload   *upload.Service   // 造图用
	recovery *recovery.Service // 被测服务
}

func newRig(t *testing.T) *rig {
	t.Helper()
	env := testenv.New(t)
	up := upload.New(upload.Config{
		Conn: env.Conn, Queries: env.Queries, Channels: env.Manager,
		Settings: testenv.Settings(nil), Logger: env.Logger,
	})
	rec := recovery.New(recovery.Config{
		Queries: env.Queries, Channels: env.Manager, Upload: up,
		Settings: testenv.Settings(nil), Logger: env.Logger,
	})
	return &rig{Env: env, upload: up, recovery: rec}
}

// pngBytes 生成一张 w×h 的真实 PNG。
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatalf("编码测试 PNG: %v", err)
	}
	return buf.Bytes()
}

// uploadImage 上传一张图并返回短 ID。
func (r *rig) uploadImage(t *testing.T, w, h int) string {
	t.Helper()
	res, err := r.upload.Process(t.Context(), pngBytes(t, w, h), upload.Options{
		UserID: sqlc.DefaultUserID, EXIFPolicy: imaging.PolicyPassthrough,
	})
	if err != nil {
		t.Fatalf("上传: %v", err)
	}
	return res.ImageID
}

// forget 删掉图片的副本状态、引用与物理图,对象留在渠道上:模拟元数据库退回到上传之前。
func (r *rig) forget(t *testing.T, imageID string) {
	t.Helper()
	blobID, err := r.Queries.GetImageBlobID(t.Context(), imageID)
	if err != nil {
		t.Fatalf("查询物理图: %v", err)
	}
	steps := []struct {
		sql string
		arg any
	}{
		{`DELETE FROM blob_channels WHERE blob_id = ?`, blobID},
		{`DELETE FROM images WHERE id = ?`, imageID},
		{`DELETE FROM blobs WHERE id = ?`, blobID},
	}
	for _, s := range steps {
		if _, err := r.Conn.ExecContext(t.Context(), s.sql, s.arg); err != nil {
			t.Fatalf("%s: %v", s.sql, err)
		}
	}
}

// put 把字节直接写进内置 local 渠道。
func (r *rig) put(t *testing.T, key string, data []byte) {
	t.Helper()
	if err := r.Local(t).Driver.Put(t.Context(), key, bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("写入对象 %q: %v", key, err)
	}
}

// TestRecoverRegistersObjectsWithoutRecord 元数据库退回后,渠道上缺记录的图按原短 ID 找回,重复执行不再登记。
func TestRecoverRegistersObjectsWithoutRecord(t *testing.T) {
	r := newRig(t)
	kept := r.uploadImage(t, 2, 2)
	lost := r.uploadImage(t, 3, 3)
	r.forget(t, lost)

	res, err := r.recovery.Recover(t.Context(), sqlc.DefaultUserID)
	if err != nil {
		t.Fatalf("找回: %v", err)
	}
	if res.Scanned != 2 || res.Recorded != 1 || res.Recovered != 1 || res.Failed != 0 {
		t.Fatalf("统计 = %+v, 期望 scanned=2 recorded=1 recovered=1 failed=0", res)
	}
	blobID, err := r.Queries.GetImageBlobID(t.Context(), lost)
	if err != nil {
		t.Fatalf("找回的图没有记录: %v", err)
	}
	blob, err := r.Queries.GetBlob(t.Context(), blobID)
	if err != nil {
		t.Fatalf("查询物理图: %v", err)
	}
	if blob.StorageKey[:id.Length] != lost || blob.Width != 3 || blob.Height != 3 || blob.RefCount != 1 {
		t.Fatalf("物理图 = %+v, 期望短 ID %s、3x3、引用 1", blob, lost)
	}
	if _, err := r.Queries.GetImageBlobID(t.Context(), kept); err != nil {
		t.Fatalf("原有记录被动过: %v", err)
	}

	again, err := r.recovery.Recover(t.Context(), sqlc.DefaultUserID)
	if err != nil {
		t.Fatalf("再次找回: %v", err)
	}
	if again.Recorded != 2 || again.Recovered != 0 || again.Failed != 0 {
		t.Fatalf("重复执行统计 = %+v, 期望 recorded=2 recovered=0", again)
	}
}

// TestRecoverReplicaStates 找回的图在持有对象的渠道记为就绪,在其余在役渠道记为待补齐。
func TestRecoverReplicaStates(t *testing.T) {
	r := newRig(t)
	other, _ := r.AddLocalChannel(t, "second", 50, false)
	lost := r.uploadImage(t, 2, 2)
	r.forget(t, lost)
	// 渠道 second 上没有对象(上传只同步写主渠道)。
	if _, err := r.recovery.Recover(t.Context(), sqlc.DefaultUserID); err != nil {
		t.Fatalf("找回: %v", err)
	}
	blobID, err := r.Queries.GetImageBlobID(t.Context(), lost)
	if err != nil {
		t.Fatalf("查询物理图: %v", err)
	}
	states := map[int64]string{}
	rows, err := r.Conn.QueryContext(t.Context(),
		`SELECT channel_id, state FROM blob_channels WHERE blob_id = ?`, blobID)
	if err != nil {
		t.Fatalf("查询副本状态: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var ch int64
		var st string
		if err := rows.Scan(&ch, &st); err != nil {
			t.Fatalf("读取副本状态: %v", err)
		}
		states[ch] = st
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历副本状态: %v", err)
	}
	if states[r.Local(t).ID] != "done" || states[other] != "pending" {
		t.Fatalf("副本状态 = %v, 期望内置渠道 done、second 渠道 pending", states)
	}
}

// TestRecoverReportsUnusableObjects 内容与键不符、不是图片的对象计入失败并给出原因,不影响其余对象;非本服务键名的对象不计。
func TestRecoverReportsUnusableObjects(t *testing.T) {
	r := newRig(t)
	good := r.uploadImage(t, 2, 2)
	r.forget(t, good)
	mismatch := id.New() + "_00000000"
	r.put(t, mismatch, pngBytes(t, 2, 2))
	garbage := id.New() + "_deadbeef"
	r.put(t, garbage, []byte("not an image"))
	r.put(t, "photos.jpg", []byte("foreign"))

	res, err := r.recovery.Recover(t.Context(), sqlc.DefaultUserID)
	if err != nil {
		t.Fatalf("找回: %v", err)
	}
	if res.Scanned != 3 || res.Recovered != 1 || res.Failed != 2 {
		t.Fatalf("统计 = %+v, 期望 scanned=3 recovered=1 failed=2", res)
	}
	reasons := map[string]string{}
	for _, f := range res.Failures {
		reasons[f.Key] = f.Reason
	}
	if reasons[mismatch] != recovery.ReasonKeyMismatch || reasons[garbage] != recovery.ReasonInvalidImage {
		t.Fatalf("失败原因 = %v", reasons)
	}
}

// TestRecoverSkipsPendingDeleteBlobs 已无引用、等着物理删除的物理图,其对象不当作缺记录重新登记。
func TestRecoverSkipsPendingDeleteBlobs(t *testing.T) {
	r := newRig(t)
	gone := r.uploadImage(t, 2, 2)
	blobID, err := r.Queries.GetImageBlobID(t.Context(), gone)
	if err != nil {
		t.Fatalf("查询物理图: %v", err)
	}
	if _, err := r.Conn.ExecContext(t.Context(), `DELETE FROM images WHERE id = ?`, gone); err != nil {
		t.Fatalf("删除引用: %v", err)
	}
	const markPending = `UPDATE blobs SET ref_count = 0, pending_delete = 1, delete_after = '2999-01-01 00:00:00'
		WHERE id = ?`
	if _, err := r.Conn.ExecContext(t.Context(), markPending, blobID); err != nil {
		t.Fatalf("标记待删除: %v", err)
	}
	res, err := r.recovery.Recover(t.Context(), sqlc.DefaultUserID)
	if err != nil {
		t.Fatalf("找回: %v", err)
	}
	if res.Recovered != 0 || res.Recorded != 1 {
		t.Fatalf("统计 = %+v, 期望待删除的物理图计为已有记录、不登记", res)
	}
}

// TestRecoverRejectsUnknownUser 归属用户不存在即拒绝,不登记。
func TestRecoverRejectsUnknownUser(t *testing.T) {
	r := newRig(t)
	if _, err := r.recovery.Recover(t.Context(), "nobody"); !errors.Is(err, recovery.ErrUserNotFound) {
		t.Fatalf("err = %v, 期望 ErrUserNotFound", err)
	}
}
