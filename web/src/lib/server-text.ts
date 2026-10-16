/**
 * 服务端返回的错误、失败结论与告警的当前语言文案:服务端只给错误码、归类与参数,文案在词典里。
 * 页面若是旧版本、服务端新增了取值,词典里查不到:错误与告警退回服务端的中文原文,失败结论退回根因原文。
 */
import { messages, t } from '@/i18n'
import type { components } from './api.gen'
import { formatBytes } from './format'

type Schemas = components['schemas']

/** 错误信封里决定文案的部分;批量结果、授权结果里嵌的失败原因与信封同形。 */
export type ErrorBody = Pick<Schemas['Error'], 'code' | 'error' | 'params'>

/** 一次失败的结论(归类与根因原文)。 */
export type Failure = Schemas['Failure']

/** 渠道最近一次失败的结论。 */
export type ChannelFailure = Schemas['ChannelFailure']

/** 告警。 */
export type AlertItem = Schemas['Alert']

/** 按键查词典;键不在词典里时为 undefined。 */
function lookup(dict: Record<string, string>, key: string): string | undefined {
  return Object.hasOwn(dict, key) ? dict[key] : undefined
}

/** 错误信封的当前语言文案。 */
export function errorText(e: ErrorBody): string {
  const template = lookup(messages().server.errors, e.code)
  return template === undefined ? e.error : t(template, e.params ?? {})
}

/** 失败结论的说明:归类说明后接根因原文;无法归类时只有原文。 */
export function failureText(f: Failure): string {
  const m = messages().server
  const reason = lookup(m.failureReasons, f.reason)
  if (
    reason === undefined ||
    (f.reason === 'unclassified' && f.detail !== '')
  ) {
    return f.detail
  }
  return f.detail === ''
    ? reason
    : t(m.failureWithDetail, { reason, detail: f.detail })
}

/** 渠道失败结论的说明:失败环节的前缀加失败结论的说明。 */
export function channelFailureText(f: ChannelFailure): string {
  const prefix = lookup(messages().server.failureStages, f.stage) ?? ''
  return prefix + failureText(f)
}

/**
 * 告警的当前语言描述,按告警类型取模板并填入参数;字节数按单位换算。
 * 类型不在词典里时退回服务端的中文描述。
 */
export function alertText(a: AlertItem): string {
  const m = messages().server
  const p = a.params
  const template = lookup(m.alerts, a.kind)
  if (template === undefined) return a.message
  switch (a.kind) {
    case 'channel_unhealthy':
    case 'drain_failed':
      if (p.reason === undefined) return m.unhealthyUnprobed
      return t(template, {
        failure: channelFailureText({
          stage: p.stage as ChannelFailure['stage'],
          reason: p.reason as Failure['reason'],
          detail: p.detail ?? '',
        }),
      })
    case 'channel_quota_exceeded':
      return t(template, {
        bytes: formatBytes(Number(p.bytes)),
        quota: formatBytes(Number(p.quota)),
      })
    case 'backup_failed': {
      const { code, ...params } = p
      return t(template, {
        error: errorText({
          code: code as ErrorBody['code'],
          error: a.message,
          params,
        }),
      })
    }
    default:
      return t(template, p)
  }
}
