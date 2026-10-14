import { z } from 'zod'
import { type Messages, t } from '@/i18n'

/** 密码最小长度;与后端 auth.MinPasswordLength 同值,后端是最终裁决,这里只为就地提示。 */
export const MIN_PASSWORD_LENGTH = 8

/** 密码字段校验:必填 + 最小长度;建号、改密、重置密码共用,门槛只定义一处。 */
export function passwordSchema(m: Messages['auth'], requiredMessage: string) {
  return z
    .string()
    .min(1, requiredMessage)
    .min(
      MIN_PASSWORD_LENGTH,
      t(m.passwordTooShort, { min: MIN_PASSWORD_LENGTH })
    )
}
