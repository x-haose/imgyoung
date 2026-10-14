import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { t } from '@/i18n'
import { Loader2 } from 'lucide-react'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/errors'
import { useMessages } from '@/context/locale-provider'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { QueryState } from '@/components/query-state'
import { BootstrapCard } from './bootstrap-card'
import { ConfigField } from './config-field'
import {
  CONFIG_FIELDS,
  CONFIG_GROUPS,
  type FieldDef,
  type FieldKey,
  type FieldValue,
  type FormValues,
  baselineValues,
  isChanged,
  toPatchValue,
  validateField,
} from './config-fields'
import { type FullConfig, fullConfigQueryOptions, updateConfig } from './data'

/** 运行配置分区:拉全量切面,就绪后渲染表单。 */
export function ConfigTab() {
  const query = useQuery(fullConfigQueryOptions())
  return (
    <QueryState query={query} skeleton={<ConfigSkeleton />}>
      {query.data && <ConfigForm config={query.data} />}
    </QueryState>
  )
}

function ConfigSkeleton() {
  return (
    <div className='grid gap-6'>
      {Array.from({ length: 3 }, (_, i) => (
        <Skeleton key={i} className='h-48 w-full rounded-xl' />
      ))}
    </div>
  )
}

/**
 * 运行配置表单:draft 只保存与基线有实质差异的字段,保存时只提交这些键;
 * 后台刷新拿到的新基线不会冲掉正在编辑的值。
 */
function ConfigForm({ config }: { config: FullConfig }) {
  const m = useMessages()
  const c = m.settings.config
  const queryClient = useQueryClient()
  const baseline = useMemo(
    () => baselineValues(config.settings),
    [config.settings]
  )
  const [draft, setDraft] = useState<Partial<FormValues>>({})
  const [rootError, setRootError] = useState('')

  const valueOf = (def: FieldDef): FieldValue =>
    draft[def.key] ?? baseline[def.key]
  const changedDefs = CONFIG_FIELDS.filter((def) => def.key in draft)
  const errors = new Map<FieldKey, string>()
  for (const def of changedDefs) {
    const error = validateField(def, valueOf(def), c)
    if (error) errors.set(def.key, error)
  }

  const setValue = (def: FieldDef, value: FieldValue) => {
    setRootError('')
    setDraft((prev) => {
      const next = { ...prev }
      if (isChanged(def, value, baseline[def.key])) next[def.key] = value
      else delete next[def.key]
      return next
    })
  }

  const applySaved = (next: FullConfig) => {
    queryClient.setQueryData(fullConfigQueryOptions().queryKey, next)
    void queryClient.invalidateQueries({ queryKey: ['server-config'] })
  }

  const save = useMutation({
    mutationFn: updateConfig,
    onSuccess: (next, patch) => {
      applySaved(next)
      setDraft({})
      toast.success(t(c.saved, { count: Object.keys(patch).length }))
    },
    onError: (err) => setRootError(errorMessage(err)),
  })

  const submit = () => {
    if (changedDefs.length === 0 || errors.size > 0) return
    const patch: Record<string, unknown> = {}
    for (const def of changedDefs) {
      patch[def.key] = toPatchValue(def, valueOf(def))
    }
    save.mutate(patch)
  }

  return (
    <form
      className='grid gap-6'
      onSubmit={(e) => {
        e.preventDefault()
        submit()
      }}
    >
      {CONFIG_GROUPS.map((group) => (
        <Card key={group.key}>
          <CardHeader>
            <CardTitle>{c.groups[group.key].title}</CardTitle>
            <CardDescription>{c.groups[group.key].hint}</CardDescription>
            {group.key === 'cdn' && (
              <CardAction>
                <CdnStatus config={config} onSaved={applySaved} />
              </CardAction>
            )}
          </CardHeader>
          <CardContent>
            <div className='grid gap-x-6 gap-y-5 sm:grid-cols-2 xl:grid-cols-3'>
              {group.fields.map((def) => (
                <ConfigField
                  key={def.key}
                  def={def}
                  value={valueOf(def)}
                  changed={def.key in draft}
                  error={errors.get(def.key)}
                  disabled={save.isPending}
                  tokenSet={config.derived.cdn_api_token_set}
                  onChange={(value) => setValue(def, value)}
                />
              ))}
            </div>
          </CardContent>
        </Card>
      ))}
      <BootstrapCard config={config} />
      <SaveBar
        changed={changedDefs.length}
        invalid={errors.size}
        rootError={rootError}
        pending={save.isPending}
        onRevert={() => {
          setDraft({})
          setRootError('')
        }}
      />
    </form>
  )
}

type SaveBarProps = {
  changed: number
  invalid: number
  rootError: string
  pending: boolean
  onRevert: () => void
}

/** 悬浮在表单底部的保存栏:改动计数、后端拒绝的原因、还原与保存。 */
function SaveBar({
  changed,
  invalid,
  rootError,
  pending,
  onRevert,
}: SaveBarProps) {
  const c = useMessages().settings.config
  return (
    <div className='sticky bottom-4 z-10 flex flex-wrap items-center gap-3 rounded-xl border bg-card/95 px-4 py-3 shadow-lg backdrop-blur'>
      <div className='flex flex-1 flex-wrap items-center gap-2 text-sm'>
        {changed === 0 ? (
          <span className='text-muted-foreground'>{c.noChanges}</span>
        ) : (
          <Badge variant='secondary'>{t(c.changes, { count: changed })}</Badge>
        )}
        {invalid > 0 && (
          <Badge variant='destructive'>
            {t(c.invalid, { count: invalid })}
          </Badge>
        )}
        {rootError ? (
          <span className='text-destructive'>{rootError}</span>
        ) : (
          changed === 0 && (
            <span className='text-xs text-muted-foreground'>{c.hotReload}</span>
          )
        )}
      </div>
      <div className='flex items-center gap-2'>
        <Button
          type='button'
          variant='outline'
          disabled={changed === 0 || pending}
          onClick={onRevert}
        >
          {c.revert}
        </Button>
        <Button
          type='submit'
          disabled={changed === 0 || invalid > 0 || pending}
        >
          {pending && <Loader2 className='animate-spin' />}
          {c.save}
        </Button>
      </div>
    </div>
  )
}

/** CDN purge 的派生状态徽标与清除 Token 入口。 */
function CdnStatus({
  config,
  onSaved,
}: {
  config: FullConfig
  onSaved: (next: FullConfig) => void
}) {
  const c = useMessages().settings.config
  const [clearing, setClearing] = useState(false)
  const clearToken = useMutation({
    mutationFn: () => updateConfig({ cdn_cloudflare_api_token: '' }),
    onSuccess: (next) => {
      onSaved(next)
      setClearing(false)
      toast.success(c.tokenCleared)
    },
  })
  const { cdn_purge_enabled: enabled, cdn_api_token_set: tokenSet } =
    config.derived
  return (
    <div className='flex flex-wrap items-center justify-end gap-2'>
      <Badge
        variant={enabled ? 'default' : 'outline'}
        className={enabled ? '' : 'text-muted-foreground'}
      >
        {enabled ? c.purgeEnabled : c.purgeDisabled}
      </Badge>
      {tokenSet && (
        <Button
          type='button'
          variant='ghost'
          size='sm'
          className='text-destructive hover:text-destructive'
          onClick={() => setClearing(true)}
        >
          {c.clearToken}
        </Button>
      )}
      <ConfirmDialog
        open={clearing}
        onOpenChange={setClearing}
        title={c.clearTokenTitle}
        desc={c.clearTokenDesc}
        confirmText={c.clearToken}
        destructive
        isLoading={clearToken.isPending}
        handleConfirm={() => clearToken.mutate()}
        className='sm:max-w-md'
      />
    </div>
  )
}
