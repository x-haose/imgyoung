// Package version 提供二进制的构建信息:发布流水线经 -ldflags -X 注入版本号、提交与构建时刻,
// 未注入的项由 Go 工具链写进二进制的模块版本与 VCS 信息补齐。
package version

import (
	"runtime/debug"
	"sync"
)

// 发布流水线以 -ldflags "-X github.com/x-haose/imgyoung/internal/infra/version.Version=…" 注入;
// 留空表示未注入。
var (
	Version string // 版本号,形如 v1.0.0
	Commit  string // 构建所在提交的完整哈希
	Date    string // 构建时刻,RFC 3339
)

// develVersion 既未注入版本号、构建信息里也没有模块版本时的占位值。
const develVersion = "devel"

// Info 二进制的构建信息。
type Info struct {
	Version string `json:"version"`  // 版本号:发布版为 git 标签;源码构建为 Go 推导的模块版本;无从得知时为 devel
	Commit  string `json:"commit"`   // 构建所在提交的哈希,工作区有未提交改动时带 -dirty 后缀;未知为空串
	BuiltAt string `json:"built_at"` // 构建时刻,RFC 3339;只有发布流水线注入,源码构建为空串
}

// String 单行文本,供 -version 输出。
// @return 形如 "imgyoung v1.0.0 (0123abcd…, 2026-01-02T15:04:05Z)",未知项省略
func (i Info) String() string {
	s := "imgyoung " + i.Version
	if i.Commit == "" && i.BuiltAt == "" {
		return s
	}
	s += " ("
	if i.Commit != "" {
		s += i.Commit
		if i.BuiltAt != "" {
			s += ", "
		}
	}
	return s + i.BuiltAt + ")"
}

// Current 返回本二进制的构建信息:注入值优先,缺项由构建信息补齐;进程内只解析一次。
var Current = sync.OnceValue(func() Info {
	info := Info{Version: Version, Commit: Commit, BuiltAt: Date}
	if bi, ok := debug.ReadBuildInfo(); ok {
		fillFromBuildInfo(&info, bi)
	}
	if info.Version == "" {
		info.Version = develVersion
	}
	return info
})

// fillFromBuildInfo 用 Go 工具链写入的模块版本与 VCS 设置补齐未注入的版本号与提交。
// @Description: 模块版本在带标签的检出或 go install …@版本 时为标签本身,否则为伪版本;
// 工具链在没有 VCS 信息时写 "(devel)",与未注入同等对待。构建时刻不从 VCS 补:
// vcs.time 是提交时刻,不是构建时刻。
// @param info 待补齐的构建信息
// @param bi 工具链写入的构建信息
func fillFromBuildInfo(info *Info, bi *debug.BuildInfo) {
	if info.Version == "" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		info.Version = bi.Main.Version
	}
	if info.Commit != "" {
		return
	}
	var revision string
	modified := false
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" {
			revision = s.Value
		}
		if s.Key == "vcs.modified" {
			modified = s.Value == "true"
		}
	}
	if revision == "" {
		return
	}
	info.Commit = revision
	if modified {
		info.Commit += "-dirty"
	}
}
