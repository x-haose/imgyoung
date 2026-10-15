# ImgYoung 文档站

基于 [Fumadocs](https://fumadocs.dev)(Next.js App Router),中英双语。技术栈:fumadocs 16 + Next.js 16 + Tailwind v4 + React 19。

## 开发

```bash
cd docs
pnpm install   # postinstall 会运行 fumadocs-mdx 生成 .source/(已忽略)
pnpm dev       # http://localhost:3000
pnpm lint      # 路由类型 + tsc + prettier
pnpm build
```

正式站是 <https://imgyoung.x-haose.com>;sitemap、canonical 与 hreflang 默认用这个域名,预览部署可用
`NEXT_PUBLIC_SITE_URL` 覆盖。

## 部署

任意能跑 Next.js 的平台均可。以 Vercel 为例:项目连本仓库,Root Directory 设为 `docs`,并保持
「Include files outside the root directory」开启——接口参考页在构建时读取仓库根的 `api/openapi.yaml`,
示例配置页引用 `deploy/` 下的文件。

## 目录

| 路径                    | 内容                                                                                         |
| ----------------------- | -------------------------------------------------------------------------------------------- |
| `content/docs/`         | 文档正文。无后缀为中文,`.en.mdx` 为英文;各级 `meta.json` / `meta.en.json` 定义导航顺序       |
| `lib/source.ts`         | 页面树:手写文档 + 由 `api/openapi.yaml` 生成的接口参考页(每个标签一页,路径取标签的 `x-slug`) |
| `lib/i18n.ts`           | 语言配置(默认 `zh`)                                                                          |
| `lib/layout.shared.tsx` | 顶栏与站点标识                                                                               |
| `app/[lang]/(home)/`    | 首页;文案在 `copy.ts`                                                                        |
| `app/llms.txt/` 等      | 给 LLM 的纯文本端点;任意文档页追加 `.md` 即得 Markdown 原文                                  |
| `app/global.css`        | 设计令牌,与管理后台同一套配色                                                                |

## 写作约定

- 新增一页:在 `content/docs/<章节>/` 建 `<页>.mdx` 与 `<页>.en.mdx`,写 frontmatter 的 `title` 与 `description`,
  再把文件名加进该章节 `meta.json` 与 `meta.en.json` 的 `pages`。
- 站内链接写相对文件路径并指向中文文件名(如 `[Docker](./docker.mdx)`),中英两版写同一个 href,会解析到同语言页面。
- 仓库里的示例配置用 `<include cwd lang="yaml">../deploy/compose.yaml</include>` 引用,不复制粘贴。
- 内容以代码为准;接口细节写进 `api/openapi.yaml`,不在正文里复述。
