# 参与贡献

感谢你愿意为 ImgYoung 出力。这份文档讲清楚怎样搭环境、改动怎样才算完成、怎样提交。设计上的「为什么」在文档站：[架构](https://imgyoung.x-haose.com/zh/docs/architecture)页是总览与技术选型，各功能页写行为与取舍（源文件在 `docs/content/docs`），改动涉及设计时先改对应页面；日常约定在 [AGENTS.md](./AGENTS.md)。

## 环境

| 工具 | 版本 | 用途 |
|---|---|---|
| Go | `go.mod` 里的版本 | 服务端；构建、测试、lint 全程 `CGO_ENABLED=0`，且带 `-tags nodynamic`（Taskfile 与 CI 经 `GOFLAGS` 设置；没有它 webp 库会让 linux 二进制依赖系统动态链接器，镜像起不来）。例外是 `task test` 的竞争检测：Linux / Windows 上它需要 cgo 与 C 编译器，Taskfile 会自动打开 |
| Node.js + pnpm | Node 24；pnpm 版本见 `web/package.json` 的 `packageManager`（`corepack enable` 即可）| 管理后台前端 |
| [Task](https://taskfile.dev) | 3 | 开发入口，`task --list` 列出全部任务 |
| golangci-lint | `.github/workflows/ci.yml` 里钉的版本 | Go lint 门禁 |
| ShellCheck | 近期任意版本 | 安装脚本 `install.sh` 的 lint |
| sqlc、atlas | 近期任意版本 | 只在改数据库 schema 或查询时需要 |

```bash
task web            # 安装前端依赖并构建产物(go:embed 需要 web/dist)
task run            # 本地跑起来,默认监听 :8721
cd web && pnpm dev  # 前端热更新,/api 与图片路径代理到 8721
```

## 仓库布局

- `cmd/imgyoung` 装配根；`internal/core` 业务；`internal/infra` 基建；`internal/http` HTTP 入站；`store/` 公开的存储引擎；`web/` 管理后台；`e2e/` 端到端测试；`api/openapi.yaml` 接口文档。
- 依赖方向 http → core → {infra, store}，store 不依赖任何 `internal` 包；depguard 在 lint 里强制。

## 改动怎样才算完成

1. Go：`goimports -w .`，`golangci-lint run` 0 issue，`go test -race ./...` 全过；涉及对外行为的改动跑 `task e2e`。
2. 前端：`cd web && pnpm lint && pnpm build`，两条都干净。
3. 改了 HTTP 接口：同步改 `api/openapi.yaml`。e2e 会按文档校验每条响应；前端类型由 `pnpm gen:api` 生成（`pnpm build` 自动执行），生成文件 `web/src/lib/api.gen.ts` 要一起提交。
4. 改了数据库：`internal/infra/db/schema/` 是唯一真相，`atlas migrate diff <名称> --env local` 生成迁移，`task generate` 重生 sqlc 代码。
5. 改了设计或对外行为：先改文档站对应页面（`docs/content/docs`，中英两份），再改代码；`cd docs && pnpm lint && pnpm build` 通过。

## 代码约定（摘要）

- 注释全覆盖：函数用 goanno 格式（首行以标识符开头），结构体字段注释写在行尾；只写现状，不写历史与讨论。
- 文件 < 800 行、函数 < 50 行（测试函数除外）、嵌套 ≤ 4 层、认知复杂度 ≤ 15、行宽 ≤ 120；lint 会拦。
- 错误如实上抛，不吞；不写 `//nolint` 之类抑制注释：改代码，确属误报的在 `.golangci.yml` 里按文件与规则排除并注明理由。
- 测试只写两类：纯计算的，以及连真实数据库 / 存储 / 队列的；流程靠 e2e，不用假件拼流程。
- 中文标点：Markdown 文档、openapi 的说明文字与后台界面词典用全角；代码注释、日志、服务端返回的文案与提交信息用半角。
- 前端：文案全部走 i18n 词典（简体中文为基准，英文同型校验）；路由文件不引 zod；请求只经 `web/src/lib/api.ts`，服务端状态只经 TanStack Query。

## 提交与 PR

- 提交信息 `<type>: <描述>`，type 取 feat / fix / refactor / docs / test / chore / perf / ci，可带作用域，如 `feat(渠道): …`；描述说清改了什么、为什么。
- 一个 PR 做一件事；描述里写明改动、原因与验证方式。CI 红了不合并。

## 发布

推送 `vX.Y.Z` 标签即触发 release 工作流：goreleaser 构建 linux / darwin / windows 的 amd64 与 arm64 二进制、GHCR 多架构镜像，并按提交类型分组生成 Release 说明。本地用 `goreleaser check` 验证配置。

## 安全问题

发现安全漏洞请不要开公开 issue，通过仓库 Security 页的 Security Advisory 私下报告。
