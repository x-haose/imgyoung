import { messages } from '@/i18n'
import { toast } from 'sonner'

/** 复制文本到剪贴板;非安全上下文或权限被拒时提示手动复制并返回 false,成功的提示由调用方决定。 */
export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    toast.error(messages().common.copyFailed)
    return false
  }
}
