import { type useFieldArray } from 'react-hook-form'
import { t } from '@/i18n'
import { Plus, Trash2 } from 'lucide-react'
import { useMessages } from '@/context/locale-provider'
import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { PasswordInput } from '@/components/password-input'
import { type FormValues } from './form-model'
import { SECRET_PLACEHOLDER } from './types'

type CustomFieldsProps = {
  rows: ReturnType<typeof useFieldArray<FormValues, 'config'>>
  isEdit: boolean
}

/**
 * 自定义模式:rclone 后端名 + 键值对参数;编辑模式说明哨兵语义。
 * 哪些参数是凭证由后端决定,这里无从判断,参数值一律以掩码输入框显示,可逐栏点开查看。
 */
export function CustomFields({ rows, isEdit }: CustomFieldsProps) {
  const m = useMessages()
  return (
    <>
      <FormField
        name='driver'
        render={({ field }) => (
          <FormItem>
            <FormLabel>{m.channels.driverLabel}</FormLabel>
            <FormControl>
              <Input placeholder={m.channels.driverPlaceholder} {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <div className='space-y-2'>
        <FormLabel>{m.channels.configLabel}</FormLabel>
        <div className='space-y-2'>
          {rows.fields.map((row, index) => (
            <div key={row.id} className='flex items-start gap-2'>
              <FormField
                name={`config.${index}.key`}
                render={({ field }) => (
                  <FormItem className='w-2/5'>
                    <FormControl>
                      <Input
                        placeholder={m.channels.configKeyPlaceholder}
                        className='font-mono text-xs'
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                name={`config.${index}.value`}
                render={({ field }) => (
                  <FormItem className='flex-1'>
                    <FormControl>
                      <PasswordInput
                        placeholder={m.channels.configValuePlaceholder}
                        autoComplete='off'
                        {...field}
                      />
                    </FormControl>
                  </FormItem>
                )}
              />
              <Button
                type='button'
                variant='ghost'
                size='icon'
                className='size-9 shrink-0 text-muted-foreground hover:text-destructive'
                onClick={() => rows.remove(index)}
              >
                <Trash2 />
                <span className='sr-only'>{m.channels.removeParam}</span>
              </Button>
            </div>
          ))}
          <Button
            type='button'
            variant='outline'
            size='sm'
            className='w-full border-dashed'
            onClick={() => rows.append({ key: '', value: '' })}
          >
            <Plus />
            {m.channels.addParam}
          </Button>
        </div>
        <p className='text-xs leading-relaxed text-muted-foreground'>
          {m.channels.configHint}
          {isEdit &&
            `${m.common.sentenceSeparator}${t(m.channels.configKeepHint, { placeholder: SECRET_PLACEHOLDER })}`}
        </p>
      </div>
    </>
  )
}
