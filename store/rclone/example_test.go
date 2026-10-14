package rclone_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/x-haose/imgyoung/store/driver"
	"github.com/x-haose/imgyoung/store/rclone"
)

// Example 用 rclone 的内存后端走一遍驱动的基本操作:写入、查询、读回、删除。
func Example() {
	ctx := context.Background()
	d, err := rclone.Open(ctx, rclone.Options{Backend: "memory", Path: "bucket"})
	if err != nil {
		fmt.Println("open:", err)
		return
	}
	defer func() { _ = d.Close(ctx) }()

	if err := d.Put(ctx, "hello.txt", strings.NewReader("hello"), 5); err != nil {
		fmt.Println("put:", err)
		return
	}
	info, err := d.Stat(ctx, "hello.txt")
	if err != nil {
		fmt.Println("stat:", err)
		return
	}
	fmt.Println(info.Key, info.Size)

	rc, err := d.Open(ctx, "hello.txt")
	if err != nil {
		fmt.Println("open object:", err)
		return
	}
	data, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		fmt.Println("read:", err)
		return
	}
	fmt.Println(string(data))

	if err := d.Delete(ctx, "hello.txt"); err != nil {
		fmt.Println("delete:", err)
		return
	}
	_, err = d.Stat(ctx, "hello.txt")
	fmt.Println(errors.Is(err, driver.ErrNotFound))
	// Output:
	// hello.txt 5
	// hello
	// true
}
