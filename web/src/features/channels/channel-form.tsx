import { useEffect, useState } from 'react'
import { useFieldArray, useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { t } from '@/i18n'
import {
  ArrowLeft,
  CheckCircle2,
  KeyRound,
  Loader2,
  XCircle,
} from 'lucide-react'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/errors'
import { useMessages } from '@/context/locale-provider'
import { useServerConfig } from '@/hooks/use-server-config'
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
import { CustomFields } from './custom-fields'
import {
  createChannel,
  testChannel,
  updateChannel,
  useInvalidateChannels,
} from './data'
import {
  buildSchema,
  defaultValues,
  type EncryptChoice,
  type EncryptionState,
  type FormValues,
  PRIORITY_MAX,
  toSource,
} from './form-model'
import { TemplateFieldInput } from './template-field-input'
import { TypeBadge } from './type-badge'
import {
  CATEGORY_DRIVE,
  type ChannelItem,
  type ChannelTemplate,
  CUSTOM,
  type EditConfig,
  SECRET_PLACEHOLDER,
  type TestResult,
  TOKEN_KEY,
} from './types'

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
  // 测试连接时服务商刷新出的令牌:已换进表单,授权字段据此显示已授权。
  const [issuedToken, setIssuedToken] = useState('')
  // 全局默认尚未取到时不据它禁用直链;冲突仍由后端拒绝。
  const inheritEncrypted = useServerConfig()?.encrypt_new_channels === true
  // 加密与 302 直链互斥(密文不能直出):编辑看渠道固有属性,创建看三态选择与全局默认。
  const encryption = (choice: EncryptChoice): EncryptionState => {
    if (channel) return channel.encrypted ? 'explicit' : 'plain'
    if (choice === 'on') return 'explicit'
    return choice === 'inherit' && inheritEncrypted ? 'inherited' : 'plain'
  }
  const form = useForm<FormValues>({
    resolver: zodResolver(buildSchema(m.channels, template, encryption)),
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

  const encryptionState = encryption(encryptChoice)
  const publicBaseURL = template
    ? (inputs.public_base_url ?? '')
    : (config.find((kv) => kv.key === 'public_base_url')?.value ?? '')
  const directLinkNeedsBaseURL = allowDirectLink && publicBaseURL.trim() === ''

  // 换进刷新后的令牌:模板模式写授权字段,自定义模式写 token 参数(没有该行则补一行)。
  const adoptToken = (token: string) => {
    const oauthKey = template?.fields.find((f) => f.type === 'oauth')?.key
    if (oauthKey) {
      form.setValue(`inputs.${oauthKey}`, token)
    } else if (!template) {
      const index = form
        .getValues('config')
        .findIndex((kv) => kv.key.trim() === TOKEN_KEY)
      if (index >= 0) form.setValue(`config.${index}.value`, token)
      else configRows.append({ key: TOKEN_KEY, value: token })
    }
    setIssuedToken(token)
  }

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
    // 换令牌会触发「修改即作废测试结论」,故先换令牌、后写结论。
    onSuccess: (result) => {
      if (result.token) adoptToken(result.token)
      setTestResult(result)
    },
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
                channelId={channel?.id}
                kept={prefill?.inputs?.[f.key] === SECRET_PLACEHOLDER}
                issuedToken={issuedToken}
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
                        <SelectItem
                          value='inherit'
                          disabled={allowDirectLink && inheritEncrypted}
                        >
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
                        {inheritEncrypted
                          ? m.channels.encryptInheritDirectLink
                          : m.channels.encryptOnDirectLink}
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
                        {encryptionState === 'inherited'
                          ? m.channels.directLinkInheritEncrypted
                          : encryptionState === 'explicit'
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
                        disabled={encryptionState !== 'plain'}
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
          {testResult?.token && (
            <p className='flex items-start gap-2 text-xs leading-relaxed text-muted-foreground'>
              <KeyRound className='mt-0.5 size-3.5 shrink-0' />
              {m.channels.testTokenRefreshed}
            </p>
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
