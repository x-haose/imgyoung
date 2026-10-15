import path from 'node:path'

import { createOpenAPI } from 'fumadocs-openapi/server'

/**
 * 接口参考的数据源:仓库里的 `api/openapi.yaml`,即服务端 HTTP 契约的唯一真相。
 *
 * 构建时读取,接口参考页由它生成,文档站不另写一份;路径相对文档站工程根目录(`docs/`)。
 */
export const openapi = createOpenAPI({
  input: [path.resolve(process.cwd(), '../api/openapi.yaml')],
})
