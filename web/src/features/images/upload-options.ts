import { toast } from 'sonner'
import { type ExifPolicy, type UploadOptions } from './data'

export type TtlMode = 'default' | 'never' | 'custom'
export type ExifMode = 'default' | ExifPolicy

/** 有效期与 EXIF 档位的表单草稿;'default' 档表示不指定,由服务端按用户与全局默认决定。 */
export interface OptionsDraft {
  ttlMode: TtlMode
  ttlHours: string // 自定义档的小时数,输入框原文
  exif: ExifMode
  private: boolean // 作为私有图保存
}

export const defaultOptionsDraft: OptionsDraft = {
  ttlMode: 'default',
  ttlHours: '24',
  exif: 'default',
  private: false,
}

/** 草稿转请求选项;自定义档未填有效小时数时提示 ttlRequiredMessage 并返回 null,静默回退到默认档会打破三态语义。 */
export function draftToOptions(
  draft: OptionsDraft,
  ttlRequiredMessage: string
): UploadOptions | null {
  const hours = Number(draft.ttlHours)
  if (draft.ttlMode === 'custom' && !(Number.isFinite(hours) && hours > 0)) {
    toast.warning(ttlRequiredMessage)
    return null
  }
  return {
    ttl:
      draft.ttlMode === 'never'
        ? 'never'
        : draft.ttlMode === 'custom'
          ? String(Math.round(hours * 3600))
          : undefined,
    exif: draft.exif === 'default' ? undefined : draft.exif,
    private: draft.private || undefined,
  }
}
