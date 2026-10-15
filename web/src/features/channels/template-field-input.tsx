import { useMessages } from '@/context/locale-provider'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { PasswordInput } from '@/components/password-input'
import { OAuthField } from './oauth-field'
import { type ChannelTemplate, type TemplateField } from './types'

type TemplateFieldInputProps = {
  field: TemplateField
  template: ChannelTemplate
  inputs: Record<string, string>
  channelId?: number // 编辑目标;创建时缺省
  kept: boolean // 编辑回显为哨兵:后端掩码的敏感值,原样提交即保留
  issuedToken: string // 服务端交还的令牌(测试连接时刷新所得);授权字段据此显示已授权
  disabled: boolean
}

/** 按模板字段类型渲染控件;回显为哨兵的敏感字段一律掩码输入,并说明哨兵语义。 */
export function TemplateFieldInput({
  field: f,
  template,
  inputs,
  channelId,
  kept,
  issuedToken,
  disabled,
}: TemplateFieldInputProps) {
  const m = useMessages()
  const masked = f.type === 'password' || kept
  const help =
    kept && f.type !== 'oauth'
      ? [f.help, m.channels.secretKeepHint].filter(Boolean).join(';')
      : f.help
  return (
    <FormField
      name={`inputs.${f.key}`}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{f.label}</FormLabel>
          {f.type === 'oauth' ? (
            <OAuthField
              template={template}
              inputs={inputs}
              channelId={channelId}
              value={field.value ?? ''}
              issuedToken={issuedToken}
              onChange={field.onChange}
              disabled={disabled}
            />
          ) : f.type === 'select' ? (
            <Select value={field.value ?? ''} onValueChange={field.onChange}>
              <FormControl>
                <SelectTrigger className='w-full'>
                  <SelectValue placeholder={m.channels.selectPlaceholder} />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                {f.options?.map((o) => (
                  <SelectItem key={o.value} value={o.value}>
                    {o.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : masked ? (
            <FormControl>
              <PasswordInput
                placeholder={f.placeholder}
                autoComplete='off'
                {...field}
                value={field.value ?? ''}
              />
            </FormControl>
          ) : (
            <FormControl>
              <Input
                placeholder={f.placeholder}
                {...field}
                value={field.value ?? ''}
              />
            </FormControl>
          )}
          {help && <FormDescription>{help}</FormDescription>}
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
