import type { Rule } from "antd/es/form";

/** 密码最小长度;与后端 auth.MinPasswordLength 同值,后端是最终裁决,这里只为就地提示。 */
export const MIN_PASSWORD_LENGTH = 8;

/** 密码输入框的校验规则:必填 + 最小长度;建号、改密、重置密码三处共用,避免门槛各写各的。 */
export function passwordRules(requiredMessage: string): Rule[] {
  return [
    { required: true, message: requiredMessage },
    { min: MIN_PASSWORD_LENGTH, message: `至少 ${MIN_PASSWORD_LENGTH} 位` },
  ];
}
