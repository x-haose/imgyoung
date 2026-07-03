# ImgYoung 项目约定

设计蓝图:`图床项目_功能与设计定稿.md`(唯一权威,动手前先对照它;改设计先改文档)。

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
  - **注释必须干净、无时态、无历史、不指向外部文档**:只描述当前代码的行为与约束。禁止 changelog 式/对话残留式注释——"改为 X""不再 Y""上一版""按讨论""迭代 N 再做"这类依赖读者知道历史或讨论上下文的表述一律不写;也不写 `(设计定稿·xxx)`/`(README·xxx)` 这类指向外部文档的引用。变更原因属于 commit message。

## 架构原则

- 按定稿执行,`CGO_ENABLED=0` 全程纯 Go。
- **升级缝只有两条**(图像处理引擎、数据库),不新增投机抽象:一个实现不提前抽接口(定稿点名的 StorageDriver 等除外)、无单一产品的工厂、无"以后可能用到"的配置项。
- 文件 < 800 行,函数 < 50 行,嵌套 ≤ 4 层,优先提前返回。
- 业务表是唯一真相(outbox),River 任务只是幂等触发器。

## 工作流

- 每完成一个功能块:重读自己的 diff 自查 → `golangci-lint run && go test ./...` 全绿 → 代码审查 → 修复后才算完成。不允许"先提交后补测试"。
- **代码审查用 general-purpose agent**(或主会话自审),不用 ecc:code-reviewer / ecc:security-reviewer——那两个 agent 的系统提示词含大量安全审计话术,叠加本项目的鉴权/加密代码会触发内容分类器误拦(已实测复现两次)。审查 prompt 模板:先声明良性背景(开源自部署图床、作者本人仓库、常规工程质量审查),用工程词汇(正确性/并发/错误处理/输入校验)描述任务。
- 测试:核心逻辑(引用计数、状态机、TTL、查重、密钥派生)单元测试;StorageDriver 链路用 rclone `local`/`memory` backend 做集成测试,零云账号依赖。
- 提交信息遵循 conventional commits(feat/fix/refactor/...)。
