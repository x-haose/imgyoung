import { readFile } from 'node:fs/promises'
import path from 'node:path'

import { createOpenAPI } from 'fumadocs-openapi/server'
import { parse } from 'yaml'

/** 示例请求指向的实例地址:文档站本身不是 ImgYoung 实例,示例按本机默认监听地址给出。 */
const EXAMPLE_SERVER = {
  url: 'http://localhost:8721',
  description: '你的 ImgYoung 实例 / your ImgYoung instance',
}

/**
 * 接口参考的数据源:仓库里的 `api/openapi.yaml`,即服务端 HTTP 契约的唯一真相;路径相对文档站工程根目录。
 *
 * 文档里的 servers 是「与站点同源」,对内嵌在二进制里的那份是对的;在文档站上渲染会把示例请求指向
 * 文档站自己,故构建时换成示例实例地址,其余原样使用。
 */
export const openapi = createOpenAPI({
  input: {
    imgyoung: async () => {
      const doc = parse(await readFile(path.resolve(process.cwd(), '../api/openapi.yaml'), 'utf8'))
      return { ...doc, servers: [EXAMPLE_SERVER] }
    },
  },
})
