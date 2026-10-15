'use client'

import { createOpenAPIPage } from 'fumadocs-openapi/ui'

/**
 * 接口参考页的渲染组件(客户端组件),数据由 openapi 虚拟页面在服务端备好后传入。
 * 不提供在线调试:请求会发往文档站而不是读者自己的实例。
 */
export const OpenAPIPage = createOpenAPIPage({ playground: { enabled: false } })
