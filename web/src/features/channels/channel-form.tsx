import { useEffect, useState } from 'react'
import { z } from 'zod'
import { useFieldArray, useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { type Messages, t } from '@/i18n'
import {
  ArrowLeft,
  CheckCircle2,
  Loader2,
  Plus,
  Trash2,
  XCircle,
} from 'lucide-react'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/errors'
import { useMessages } from '@/context/locale-provider'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Form,
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
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { PasswordInput } from '@/components/password-input'
import {
  type ChannelSource,
  createChannel,
  testChannel,
  updateChannel,
  useInvalidateChannels,
} from './data'
import { OAuthField } from './oauth-field'
import { TypeBadge } from './type-badge'
import {
  CATEGORY_DRIVE,
  type ChannelItem,
  type ChannelTemplate,
  CUSTOM,
  type EditConfig,
  SECRET_PLACEHOLDER,
  type TemplateField,
  type TestResult,
} from './types'

/** 创建时服务端加密的三态;继承态不提交该字段,由后端按全局默认固化。 */
type EncryptChoice = 'inherit' | 'on' | 'off'

type ConfigEntry = { key: string; value: string }

type FormValues = {
  name: string
  priority: number
  encrypted: EncryptChoice
  allow_direct_link: boolean
  auto_create_bucket: boolean
  inputs: Record<string, string> // 模板字段值,键为字段 key
  driver: string // 自定义模式
  config: ConfigEntry[] // 自定义模式
}

const PRIORITY_MAX = 1000
const NAME_MAX_BYTES = 64

/** 校验规则随模板变化:模板必填字段逐个检查,自定义模式检查驱动名与参数键。 */
function buildSchema(
  m: Messages['channels'],
  template: ChannelTemplate | null
) {
  const priorityMessage = m.priorityRange
  return z
    .object({
      name: z
        .string()
        .trim()
        .min(1, m.nameRequired)
        .refine(
          (v) => new TextEncoder().encode(v).length <= NAME_MAX_BYTES,
          m.nameTooLong
        ),
      priority: z
        .number({ error: priorityMessage })
        .int(priorityMessage)
        .min(0, priorityMessage)
        .max(PRIORITY_MAX, priorityMessage),
      encrypted: z.enum(['inherit', 'on', 'off']),
      allow_direct_link: z.boolean(),
      auto_create_bucket: z.boolean(),
      inputs: z.record(z.string(), z.string()),
      driver: z.string(),
      config: z.array(z.object({ key: z.string(), value: z.string() })),
    })
    .superRefine((v, ctx) => {
      if (template) {
        for (const f of template.fields) {
          if (f.required && (v.inputs[f.key] ?? '').trim() === '') {
            ctx.addIssue({
              code: 'custom',
              path: ['inputs', f.key],
              message: t(m.fieldRequired, { label: f.label }),
            })
          }
        }
      } else {
        if (v.driver.trim() === '') {
          ctx.addIssue({
            code: 'custom',
            path: ['driver'],
            message: m.driverRequired,
          })
        }
        const seen = new Set<string>()
        v.config.forEach((kv, i) => {
          const key = kv.key.trim()
          if (key === '') {
            ctx.addIssue({
              code: 'custom',
              path: ['config', i, 'key'],
              message: m.configKeyRequired,
            })
          } else if (seen.has(key)) {
            ctx.addIssue({
              code: 'custom',
              path: ['config', i, 'key'],
              message: m.configKeyDuplicate,
            })
          }
          seen.add(key)
        })
      }
      if (v.encrypted === 'on' && v.allow_direct_link) {
        ctx.addIssue({
          code: 'custom',
          path: ['allow_direct_link'],
          message: m.directLinkEncrypted,
        })
      }
    })
}

/** 模板字段全部以受控空串起步,未碰过的字段不会以 undefined 进入校验。 */
function defaultValues(
  template: ChannelTemplate | null,
  channel: ChannelItem | null,
  prefill: EditConfig | undefined
): FormValues {
  const inputs: Record<string, string> = {}
  for (const f of template?.fields ?? []) {
    inputs[f.key] = prefill?.inputs?.[f.key] ?? ''
  }
  return {
    name: channel?.name ?? '',
    priority: channel?.priority ?? 100,
    encrypted: 'inherit',
    allow_direct_link: channel?.allow_direct_link ?? false,
    auto_create_bucket: false,
    inputs,
    driver: prefill?.driver ?? '',
    config: Object.entries(prefill?.config ?? {}).map(([key, value]) => ({
      key,
      value,
    })),
  }
}

/** 配置部分的请求体:模板模式提交字段值,自定义模式提交驱动与参数(去掉空键)。 */
function toSource(
  values: FormValues,
  template: ChannelTemplate | null
): ChannelSource {
  if (template) {
    const inputs: Record<string, string> = {}
    for (const f of template.fields) {
      const v = (values.inputs[f.key] ?? '').trim()
      if (v !== '') inputs[f.key] = v
    }
    return { template: template.id, inputs }
  }
  const config: Record<string, string> = {}
  for (const kv of values.config) {
    const key = kv.key.trim()
    if (key !== '') config[key] = kv.value
  }
  return { template: CUSTOM, driver: values.driver.trim(), config }
}

type ChannelFormProps = {
  template: ChannelTemplate | null // null 为自定义模式
  channel: ChannelItem | null // 编辑目标;null 为创建
  prefill?: EditConfig // 编辑回显,敏感值为哨兵
  onBack?: () => void // 回到选类型步;编辑模式不提供
  onSaved: () => void
  onCancel: () => void
}

/**
 * 渠道配置表单:按模板渲染专属字段,或自定义模式直填 rclone 参数;
 * 两种模式都可先「测试连接」做不落库探测,编辑模式带渠道主键让后端把哨兵还原为原凭证后再测。
 */
export function ChannelForm({
  template,
  channel,
  prefill,
  onBack,
  onSaved,
  onCancel,
}: ChannelFormProps) {
  const m = useMessages()
  const invalidate = useInvalidateChannels()
  const isEdit = channel !== null
  const isDrive = template?.category === CATEGORY_DRIVE
  const [testResult, setTestResult] = useState<TestResult | null>(null)
  const form = useForm<FormValues>({
    resolver: zodResolver(buildSchema(m.channels, template)),
    defaultValues: defaultValues(template, channel, prefill),
  })
  const configRows = useFieldArray({ control: form.control, name: 'config' })
  const inputs = form.watch('inputs')
  const config = form.watch('config')
  const allowDirectLink = form.watch('allow_direct_link')
  const encryptChoice = form.watch('encrypted')

  // 任何修改都让上一次测试结论作废。
  useEffect(() => {
    const sub = form.watch(() => setTestResult(null))
    return () => sub.unsubscribe()
  }, [form])

  // 加密与 302 直链互斥(密文不能直出):创建看三态选择,编辑看渠道固有属性。
  const encryptedOn = isEdit ? channel.encrypted : encryptChoice === 'on'
  const publicBaseURL = template
    ? (inputs.public_base_url ?? '')
    : (config.find((kv) => kv.key === 'public_base_url')?.value ?? '')
  const directLinkNeedsBaseURL = allowDirectLink && publicBaseURL.trim() === ''

  const test = useMutation({
    mutationFn: (values: FormValues) =>
      testChannel({
        ...toSource(values, template),
        name: values.name,
        priority: values.priority,
        allow_direct_link: values.allow_direct_link,
        auto_create_bucket: values.auto_create_bucket,
        ...(channel ? { channel_id: channel.id } : {}),
      }),
    onSuccess: setTestResult,
    onError: (err) => setTestResult({ ok: false, error: errorMessage(err) }),
  })

  const save = useMutation({
    mutationFn: (values: FormValues) => {
      const source = toSource(values, template)
      if (channel) {
        return updateChannel(channel.id, {
          ...source,
          name: values.name,
          priority: values.priority,
          allow_direct_link: values.allow_direct_link,
        })
      }
      return createChannel({
        ...source,
        name: values.name,
        priority: values.priority,
        allow_direct_link: values.allow_direct_link,
        auto_create_bucket: values.auto_create_bucket,
        ...(values.encrypted === 'inherit'
          ? {}
          : { encrypted: values.encrypted === 'on' }),
      })
    },
    onSuccess: (info) => {
      if (info.last_error) {
        toast.warning(
          t(
            isEdit
              ? m.channels.savedProbeFailed
              : m.channels.createdProbeFailed,
            {
              error: info.last_error,
            }
          )
        )
      } else {
        toast.success(isEdit ? m.channels.saved : m.channels.created)
      }
      invalidate()
      onSaved()
    },
    onError: (err) => form.setError('root', { message: errorMessage(err) }),
  })

  const runTest = async () => {
    if (!(await form.trigger())) return
    test.mutate(form.getValues())
  }
  const busy = test.isPending || save.isPending

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit((values) => save.mutate(values))}
        className='flex min-h-0 flex-1 flex-col'
      >
        <div className='min-h-0 flex-1 space-y-5 overflow-y-auto px-4 pt-4 pb-5'>
          <TypeHeader template={template} onBack={onBack} />

          <FormField
            control={form.control}
            name='name'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{m.channels.nameLabel}</FormLabel>
                <FormControl>
                  <Input
                    placeholder={m.channels.namePlaceholder}
                    autoFocus={!isEdit}
                    {...field}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          {template ? (
            template.fields.map((f) => (
              <TemplateFieldInput
                key={f.key}
                field={f}
                template={template}
                inputs={inputs}
                kept={prefill?.inputs?.[f.key] === SECRET_PLACEHOLDER}
                disabled={busy}
              />
            ))
          ) : (
            <CustomFields rows={configRows} isEdit={isEdit} />
          )}

          <Separator />

          <div className='grid gap-5 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='priority'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{m.channels.priorityLabel}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      max={PRIORITY_MAX}
                      value={Number.isNaN(field.value) ? '' : field.value}
                      onChange={(e) =>
                        field.onChange(
                          e.target.value === '' ? NaN : e.target.valueAsNumber
                        )
                      }
                      onBlur={field.onBlur}
                      name={field.name}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>{m.channels.priorityHint}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            {isEdit ? (
              <div className='space-y-2'>
                <FormLabel>{m.channels.encryptLabel}</FormLabel>
                <div className='flex h-9 items-center'>
                  <Badge variant={channel.encrypted ? 'default' : 'secondary'}>
                    {channel.encrypted
                      ? m.channels.encryptedYes
                      : m.channels.encryptedNo}
                  </Badge>
                </div>
                <p className='text-xs text-muted-foreground'>
                  {m.channels.encryptedImmutable}
                </p>
              </div>
            ) : (
              <FormField
                control={form.control}
                name='encrypted'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{m.channels.encryptLabel}</FormLabel>
                    <Select value={field.value} onValueChange={field.onChange}>
                      <FormControl>
                        <SelectTrigger className='w-full'>
                          <SelectValue />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent>
                        <SelectItem value='inherit'>
                          {m.channels.encryptInherit}
                        </SelectItem>
                        <SelectItem value='on' disabled={allowDirectLink}>
                          {m.channels.encryptOn}
                        </SelectItem>
                        <SelectItem value='off'>
                          {m.channels.encryptOff}
                        </SelectItem>
                      </SelectContent>
                    </Select>
                    {allowDirectLink && (
                      <FormDescription>
                        {m.channels.encryptOnDirectLink}
                      </FormDescription>
                    )}
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}
          </div>

          {!isDrive && (
            <FormField
              control={form.control}
              name='allow_direct_link'
              render={({ field }) => (
                <FormItem className='rounded-lg border p-3'>
                  <div className='flex items-start justify-between gap-4'>
                    <div className='space-y-1'>
                      <FormLabel>{m.channels.directLinkLabel}</FormLabel>
                      <FormDescription>
                        {encryptedOn
                          ? m.channels.directLinkEncrypted
                          : directLinkNeedsBaseURL
                            ? m.channels.directLinkNeedsBaseURL
                            : m.channels.directLinkHint}
                      </FormDescription>
                    </div>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                        disabled={encryptedOn}
                      />
                    </FormControl>
                  </div>
                  <FormMessage />
                </FormItem>
              )}
            />
          )}

          {!isEdit && !isDrive && (
            <FormField
              control={form.control}
              name='auto_create_bucket'
              render={({ field }) => (
                <FormItem className='rounded-lg border p-3'>
                  <div className='flex items-start justify-between gap-4'>
                    <div className='space-y-1'>
                      <FormLabel>{m.channels.autoCreateBucketLabel}</FormLabel>
                      <FormDescription>
                        {m.channels.autoCreateBucketHint}
                      </FormDescription>
                    </div>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </div>
                </FormItem>
              )}
            />
          )}
        </div>

        <div className='space-y-3 border-t p-4'>
          {testResult && (
            <Alert
              variant={testResult.ok ? 'default' : 'destructive'}
              className={testResult.ok ? 'border-success/40 text-success' : ''}
            >
              {testResult.ok ? <CheckCircle2 /> : <XCircle />}
              <AlertDescription className={testResult.ok ? 'text-success' : ''}>
                {testResult.ok
                  ? m.channels.testOk
                  : t(m.channels.testFailed, { error: testResult.error ?? '' })}
              </AlertDescription>
            </Alert>
          )}
          {form.formState.errors.root && (
            <p className='text-sm text-destructive'>
              {form.formState.errors.root.message}
            </p>
          )}
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <Button
              type='button'
              variant='outline'
              disabled={busy}
              onClick={runTest}
            >
              {test.isPending && <Loader2 className='animate-spin' />}
              {m.channels.testConnection}
            </Button>
            <div className='flex gap-2'>
              <Button type='button' variant='ghost' onClick={onCancel}>
                {m.common.cancel}
              </Button>
              <Button type='submit' disabled={busy}>
                {save.isPending && <Loader2 className='animate-spin' />}
                {isEdit ? m.common.save : m.channels.createSubmit}
              </Button>
            </div>
          </div>
        </div>
      </form>
    </Form>
  )
}

/** 配置步的头:类型徽标、名称与说明;创建模式可返回重选类型。 */
function TypeHeader({
  template,
  onBack,
}: {
  template: ChannelTemplate | null
  onBack?: () => void
}) {
  const m = useMessages()
  return (
    <div className='flex items-center gap-3'>
      {onBack && (
        <Button
          type='button'
          variant='ghost'
          size='icon'
          className='size-8'
          onClick={onBack}
        >
          <ArrowLeft />
          <span className='sr-only'>{m.common.back}</span>
        </Button>
      )}
      <TypeBadge id={template?.id ?? CUSTOM} />
      <div className='min-w-0'>
        <p className='truncate text-sm font-medium'>
          {template?.name ?? m.channels.customName}
        </p>
        <p className='truncate text-xs text-muted-foreground'>
          {template?.description ?? m.channels.customDesc}
        </p>
      </div>
    </div>
  )
}

type TemplateFieldInputProps = {
  field: TemplateField
  template: ChannelTemplate
  inputs: Record<string, string>
  kept: boolean // 编辑回显为哨兵:后端掩码的敏感值,原样提交即保留
  disabled: boolean
}

/** 按模板字段类型渲染控件;回显为哨兵的敏感字段一律掩码输入,并说明哨兵语义。 */
function TemplateFieldInput({
  field: f,
  template,
  inputs,
  kept,
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
              value={field.value ?? ''}
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

type CustomFieldsProps = {
  rows: ReturnType<typeof useFieldArray<FormValues, 'config'>>
  isEdit: boolean
}

/** 自定义模式:rclone 后端名 + 键值对参数;编辑模式说明哨兵语义。 */
function CustomFields({ rows, isEdit }: CustomFieldsProps) {
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
                      <Input
                        placeholder={m.channels.configValuePlaceholder}
                        className='font-mono text-xs'
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
            `;${t(m.channels.configKeepHint, { placeholder: SECRET_PLACEHOLDER })}`}
        </p>
      </div>
    </>
  )
}
