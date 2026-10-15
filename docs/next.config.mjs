import path from 'node:path'

import { createMDX } from 'fumadocs-mdx/next'

const withMDX = createMDX()

/** @type {import('next').NextConfig} */
const config = {
  reactStrictMode: true,
  /**
   * 以仓库根为构建根:正文经 <include> 引用仓库里的示例配置与示例代码(deploy/、store/),
   * 接口参考读 api/openapi.yaml,这些文件都在文档站工程目录之外。
   */
  turbopack: { root: path.join(import.meta.dirname, '..') },
  /** 不自动生成 AGENTS.md / CLAUDE.md,仓库的约定文件由项目自己维护。 */
  agentRules: false,
  /**
   * 任意文档页追加 `.md` 或 `.mdx` 即得到该页的 Markdown 原文,
   * 供页面上的复制按钮与外部 AI 代理直接抓取;`Accept: text/markdown` 的协商见 proxy.ts。
   */
  async rewrites() {
    return [
      { source: '/:lang/docs/:path*.md', destination: '/llms.mdx/:lang/docs/:path*' },
      { source: '/:lang/docs/:path*.mdx', destination: '/llms.mdx/:lang/docs/:path*' },
    ]
  },
}

export default withMDX(config)
