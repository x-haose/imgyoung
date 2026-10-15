package failover_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/x-haose/imgyoung/store/driver"
	"github.com/x-haose/imgyoung/store/failover"
	"github.com/x-haose/imgyoung/store/rclone"
)

// Example 两个本地目录充当主、备存储,主存储故障时写入顺延到备存储,读取同样切到备存储。
func Example() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "failover-example")
	if err != nil {
		fmt.Println("mkdir:", err)
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()

	// 主存储的根落在一个普通文件之下,任何写入都会失败,以此模拟主存储故障。
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		fmt.Println("write blocker:", err)
		return
	}
	primary, err := rclone.Open(ctx, rclone.Options{Backend: "local", Path: filepath.Join(blocker, "data")})
	if err != nil {
		fmt.Println("open primary:", err)
		return
	}
	defer func() { _ = primary.Close(ctx) }()
	backup, err := rclone.Open(ctx, rclone.Options{Backend: "local", Path: filepath.Join(dir, "backup")})
	if err != nil {
		fmt.Println("open backup:", err)
		return
	}
	defer func() { _ = backup.Close(ctx) }()
	ds := []driver.Driver{primary, backup} // 顺序即优先级

	data := []byte("hello")
	written, err := failover.Write(ctx, ds, "a.txt", bytes.NewReader(data), int64(len(data)),
		failover.WriteOptions{Need: 1})
	if err != nil {
		fmt.Println("write:", err)
		return
	}
	for _, a := range written {
		fmt.Println("write to", a.Index, "ok:", a.Err == nil)
	}

	rc, read, err := failover.Open(ctx, ds, "a.txt")
	if err != nil {
		fmt.Println("read:", err)
		return
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		fmt.Println("read all:", err)
		return
	}
	fmt.Println(string(got), "from", read[len(read)-1].Index)
	// Output:
	// write to 0 ok: false
	// write to 1 ok: true
	// hello from 1
}
