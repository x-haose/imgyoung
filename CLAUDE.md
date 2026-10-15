# ImgYoung 项目约定

设计蓝图:`图床项目_功能与设计定稿.md`(唯一权威,动手前先对照它;改设计先改文档)。

## 决策标准(最高优先)

- 代码的功能、质量、架构、干净程度(即「强大」)永远是第一优先。
- **绝不以「最小改动 / 最小破坏」为标准做决定**;只做对的决定,不做最小的。
- 某块/某服务/整体到了重写更优的程度,只要收益高就重写,在所不惜。
- 零容忍对付、补丁、将就式编码;取当前范围内最合适、最高的标准。

## 质量红线(不可协商)

- **0 警告**:`golangci-lint run` 0 issue、`go vet` 0 警告、GoLand IDE 0 警告。lint 配置在 `.golangci.yml`(迭代 0 建立,linter 集合从严),CI 门禁,红了不合并。`//nolint` 必须附一行理由,否则视为违规。
- **注释全覆盖**:接口、方法、函数、结构体、字段——导出与非导出的都要有注释。
  - 函数/方法用 **goanno 格式**,但**首行必须以标识符名开头**(兼容 godoc 与 revive 的 exported 规则),随后接 `@Description` / `@param` / `@return`:

    ```go
    // Put 将字节流写入指定渠道。
    // @Description: 流式上传,不落盘;调用方负责关闭 r。渠道不可用时返回 ErrChannelDown。
    // @param ctx 控制超时与取消
    // @param key 渠道内对象键
    // @param r 待写入的数据流
    // @return 上传后的对象信息
    // @return 错误
    func (d *RcloneDriver) Put(ctx context.Context, key string, r io.Reader) (*ObjectInfo, error)
    ```

  - **结构体字段注释一律写在字段行尾(右侧)**,不带结尾句号,不重复字段名,说明语义、单位、边界(如`ExpireAt time.Time // 到期时刻,UTC;零值表示未认领`)。接口方法、函数、包等仍用上方注释。
  - 注释解释**为什么和约束**,不复述代码在做什么。
  - **注释必须干净、无时态、无历史、不指向外部文档**:只描述当前代码的行为与约束,必须自包含——不依赖对话、设计文档章节号、changelog 才能读懂。禁止 changelog 式/对话残留式注释——"改为 X""不再 Y""上一版""按讨论""迭代 N 再做"这类依赖读者知道历史或讨论上下文的表述一律不写;也不写 `(设计定稿·xxx)`/`(README·xxx)` 这类指向外部文档的引用。变更原因属于 commit message。
  - **SQL schema 注释全覆盖**:每张表上方 `--` 注释,每一列行尾 `--` 注释(SQLite 无 `COMMENT ON`,以 DDL 内注释为等价实现)。

## 架构原则

- 按定稿执行,`CGO_ENABLED=0` 全程纯 Go;所有二进制构建带 `-tags nodynamic`(Taskfile 与 CI 经 `GOFLAGS` 设置),否则 webp 库会让 linux 二进制依赖系统动态链接器,distroless/static 镜像起不来。
- **升级缝只有两条**(图像处理引擎、数据库),不新增投机抽象:一个实现不提前抽接口(定稿点名的 StorageDriver 等除外)、无单一产品的工厂、无"以后可能用到"的配置项。
- 文件 < 800 行,函数 < 50 行,嵌套 ≤ 4 层,优先提前返回。
- 业务表是唯一真相(outbox),River 任务只是幂等触发器。
- **接口契约**:`api/openapi.yaml` 是对外 HTTP 契约的唯一真相,改接口先改它;e2e 经契约校验器核对每条响应,前端类型由 `pnpm gen:api` 生成(`pnpm build` 自动执行,产物 `web/src/lib/api.gen.ts` 入库)。

## 前端质量(管理后台)

- **界面观感是硬性验收项**:不接受组件库默认样式直出的"模板脸";层级、间距、排版、色彩、hover/focus 状态必须有设计意图,要像真实产品截图,而非脚手架 demo。
- 前端代码同守质量红线:组件小而聚焦、设计 token 统一管理,不散落硬编码样式值。
- 技术栈:React 19 + TanStack Router(文件路由,`web/src/routes`)+ TanStack Query + shadcn/ui + Tailwind v4;门禁 `cd web && pnpm lint`(tsc + eslint + prettier)零输出,`pnpm build` 产物进 `web/dist` 供 go:embed。
- 文案全部走 i18n 词典(`web/src/i18n`,简体中文为基准、英文同型校验),TSX 里不出现硬编码文案;路由文件不引入 zod(搜索参数用 `web/src/lib/search.ts`,eslint 已禁),表单校验才用 zod;请求只经 `web/src/lib/api.ts`,服务端状态只经 TanStack Query。

## 工作流

- **每次改完 Go 代码立即跑** `goimports -w .` + `golangci-lint run`(linter 集合已含 go vet / staticcheck / goimports 校验),import 与格式必须干净、0 告警才继续;不把格式化与告警攒到提交前。
- 每完成一个功能块:重读自己的 diff 自查 → `golangci-lint run && go test ./...` 全绿 → 代码审查 → 修复后才算完成。不允许"先提交后补测试"。
- **代码审查用 general-purpose agent**(或主会话自审),不用 ecc:code-reviewer / ecc:security-reviewer——那两个 agent 的系统提示词含大量安全审计话术,叠加本项目的鉴权/加密代码会触发内容分类器误拦(已实测复现两次)。审查 prompt 模板:先声明良性背景(开源自部署图床、作者本人仓库、常规工程质量审查),用工程词汇(正确性/并发/错误处理/输入校验)描述任务。
- 测试:核心逻辑(引用计数、状态机、TTL、查重、密钥派生)单元测试;StorageDriver 链路用 rclone `local`/`memory` backend 做集成测试,零云账号依赖。
- 提交信息遵循 conventional commits(feat/fix/refactor/...)。
