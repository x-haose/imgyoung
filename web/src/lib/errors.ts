import { messages } from '@/i18n'
import { toast } from 'sonner'
import { ApiError } from './api'

/** 用户可读的失败原因:API 错误取后端文案,其余 Error 取 message,没有消息时用通用文案。 */
export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message
  if (err instanceof Error && err.message) return err.message
  return messages().common.unknownError
}

export function toastError(err: unknown): void {
  toast.error(errorMessage(err))
}

/** 是否为调用方经 AbortSignal 主动中止的请求;这类失败不是故障,不提示。 */
export function isAbort(err: unknown): boolean {
  return err instanceof DOMException && err.name === 'AbortError'
}

export function hasStatus(err: unknown, status: number): boolean {
  return err instanceof ApiError && err.status === status
}
