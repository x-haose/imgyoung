import { useState } from 'react'
import { z } from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { t } from '@/i18n'
import { Loader2, Plus, X } from 'lucide-react'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/errors'
import { useMessages } from '@/context/locale-provider'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { QueryState } from '@/components/query-state'
import {
  type RefererRule,
  addRefererRule,
  fullConfigQueryOptions,
  refererQueryOptions,
  removeRefererRule,
  updateConfig,
} from './data'

/** 规则语法,与后端 referer 包一致:可选 *. 前缀 + 一至多段主机名,不含 scheme、路径与端口。 */
const PATTERN_RE =
  /^(\*\.)?[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$/

/** 防盗链分区:空 Referer 策略开关、生效规则与添加表单,增删即时生效。 */
export function RefererTab() {
  const m = useMessages()
  const r = m.settings.referer
  const queryClient = useQueryClient()
  const rules = useQuery(refererQueryOptions())
  const config = useQuery(fullConfigQueryOptions())
  const [removing, setRemoving] = useState<RefererRule | null>(null)

  const invalidateRules = () =>
    queryClient.invalidateQueries({ queryKey: refererQueryOptions().queryKey })

  const remove = useMutation({
    mutationFn: (rule: RefererRule) => removeRefererRule(rule.id),
    onSuccess: (_, rule) => {
      setRemoving(null)
      toast.success(t(r.removed, { pattern: rule.pattern }))
      return invalidateRules()
    },
  })

  const allowEmpty = useMutation({
    mutationFn: (allow: boolean) =>
      updateConfig({ allow_empty_referer: allow }),
    onSuccess: (next) => {
      queryClient.setQueryData(fullConfigQueryOptions().queryKey, next)
      void queryClient.invalidateQueries({ queryKey: ['server-config'] })
      toast.success(r.allowEmptySaved)
    },
  })

  const items = rules.data?.items ?? []
  return (
    <Card className='max-w-3xl'>
      <CardHeader>
        <CardTitle>{r.title}</CardTitle>
        <CardDescription>{r.desc}</CardDescription>
      </CardHeader>
      <CardContent className='grid gap-6'>
        <div className='flex items-start justify-between gap-4 rounded-lg border bg-muted/30 p-4'>
          <div className='grid gap-1'>
            <Label htmlFor='allow-empty-referer'>{r.allowEmpty}</Label>
            <p className='text-xs leading-relaxed text-muted-foreground'>
              {r.allowEmptyHint}
            </p>
          </div>
          <Switch
            id='allow-empty-referer'
            checked={config.data?.settings.allow_empty_referer ?? false}
            disabled={!config.data || allowEmpty.isPending}
            onCheckedChange={(checked) => allowEmpty.mutate(checked)}
          />
        </div>

        <QueryState
          query={rules}
          skeleton={<Skeleton className='h-16 w-full rounded-lg' />}
        >
          <section className='grid gap-3'>
            <h3 className='text-sm font-medium'>{r.rules}</h3>
            {items.length === 0 ? (
              <p className='text-sm text-muted-foreground'>{r.disabled}</p>
            ) : (
              <ul className='flex flex-wrap gap-2'>
                {items.map((rule) => (
                  <li key={rule.id}>
                    <RuleChip rule={rule} onRemove={() => setRemoving(rule)} />
                  </li>
                ))}
              </ul>
            )}
          </section>
        </QueryState>

        <AddRuleForm onAdded={invalidateRules} />
      </CardContent>

      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(open) => !open && setRemoving(null)}
        title={t(r.removeTitle, { pattern: removing?.pattern ?? '' })}
        desc={r.removeDesc}
        confirmText={m.common.delete}
        destructive
        isLoading={remove.isPending}
        handleConfirm={() => removing && remove.mutate(removing)}
        className='sm:max-w-md'
      />
    </Card>
  )
}

function RuleChip({
  rule,
  onRemove,
}: {
  rule: RefererRule
  onRemove: () => void
}) {
  const r = useMessages().settings.referer
  return (
    <span className='inline-flex items-center gap-1 rounded-md border bg-card py-1 ps-2.5 pe-1 font-mono text-sm'>
      {rule.pattern}
      <button
        type='button'
        aria-label={t(r.remove, { pattern: rule.pattern })}
        className='rounded-sm p-0.5 text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none'
        onClick={onRemove}
      >
        <X className='size-3.5' />
      </button>
    </span>
  )
}

type AddValues = { pattern: string }

function AddRuleForm({ onAdded }: { onAdded: () => unknown }) {
  const r = useMessages().settings.referer
  const schema = z.object({
    pattern: z
      .string()
      .trim()
      .toLowerCase()
      .regex(PATTERN_RE, r.patternInvalid),
  })
  const form = useForm<AddValues>({
    resolver: zodResolver(schema),
    defaultValues: { pattern: '' },
  })
  const add = useMutation({
    mutationFn: (v: AddValues) => addRefererRule(v.pattern),
    onSuccess: () => {
      form.reset()
      toast.success(r.added)
      return onAdded()
    },
    onError: (err) => form.setError('root', { message: errorMessage(err) }),
  })
  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit((v) => add.mutate(v))}
        className='grid gap-2'
      >
        <FormField
          control={form.control}
          name='pattern'
          render={({ field }) => (
            <FormItem>
              <div className='flex flex-col gap-2 sm:flex-row'>
                <FormControl>
                  <Input
                    placeholder={r.patternPlaceholder}
                    autoComplete='off'
                    spellCheck={false}
                    className='font-mono sm:max-w-xs'
                    {...field}
                  />
                </FormControl>
                <Button type='submit' disabled={add.isPending}>
                  {add.isPending ? (
                    <Loader2 className='animate-spin' />
                  ) : (
                    <Plus />
                  )}
                  {r.add}
                </Button>
              </div>
              <FormDescription>{r.syntax}</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        {form.formState.errors.root && (
          <p className='text-sm text-destructive'>
            {form.formState.errors.root.message}
          </p>
        )}
      </form>
    </Form>
  )
}
