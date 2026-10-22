import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { t } from '@/i18n'
import { Loader2, Play } from 'lucide-react'
import { toast } from 'sonner'
import { formatNumber } from '@/lib/format'
import { cn } from '@/lib/utils'
import { useMessages } from '@/context/locale-provider'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { QueryState } from '@/components/query-state'
import {
  importHostsQueryOptions,
  importsKey,
  startImport,
  type ImportHost,
} from './data'

/** 确认页:按域名分组列出待导入的图片地址,勾选要搬的域名后开始导入;默认一个都不选。 */
export function HostPicker({ id }: { id: string }) {
  const m = useMessages().imports.hosts
  const queryClient = useQueryClient()
  const query = useQuery(importHostsQueryOptions(id))
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const hosts = query.data ?? []
  const images = hosts
    .filter((h) => selected.has(h.host))
    .reduce((sum, h) => sum + h.count, 0)

  const start = useMutation({
    mutationFn: () => startImport(id, [...selected]),
    onSuccess: () => {
      toast.success(m.started)
      return queryClient.invalidateQueries({ queryKey: importsKey })
    },
  })
  const toggle = (host: string) =>
    setSelected((prev) => {
      const next = new Set(prev)
      if (!next.delete(host)) next.add(host)
      return next
    })

  return (
    <Card>
      <CardHeader>
        <CardTitle>{m.title}</CardTitle>
        <CardDescription>{m.desc}</CardDescription>
      </CardHeader>
      <CardContent>
        <QueryState query={query}>
          <div className='mb-3 flex gap-1'>
            <Button
              variant='ghost'
              size='sm'
              onClick={() => setSelected(new Set(hosts.map((h) => h.host)))}
            >
              {m.selectAll}
            </Button>
            <Button
              variant='ghost'
              size='sm'
              disabled={selected.size === 0}
              onClick={() => setSelected(new Set())}
            >
              {m.clear}
            </Button>
          </div>
          <ul className='divide-y overflow-hidden rounded-xl border'>
            {hosts.map((h) => (
              <HostRow
                key={h.host}
                host={h}
                checked={selected.has(h.host)}
                onToggle={() => toggle(h.host)}
              />
            ))}
          </ul>
        </QueryState>
      </CardContent>
      <CardFooter className='flex-wrap justify-between gap-3'>
        <p className='text-sm text-muted-foreground'>
          {selected.size === 0
            ? m.none
            : t(m.selected, {
                hosts: formatNumber(selected.size),
                images: formatNumber(images),
              })}
        </p>
        <Button
          disabled={selected.size === 0 || start.isPending}
          onClick={() => start.mutate()}
        >
          {start.isPending ? <Loader2 className='animate-spin' /> : <Play />}
          {m.start}
        </Button>
      </CardFooter>
    </Card>
  )
}

/** 一个域名一行:整行可点,示例地址只显示文本,后台不加载外部图片。 */
function HostRow({
  host,
  checked,
  onToggle,
}: {
  host: ImportHost
  checked: boolean
  onToggle: () => void
}) {
  const m = useMessages().imports.hosts
  return (
    <li>
      <label
        className={cn(
          'flex cursor-pointer items-start gap-3 px-4 py-3 transition-colors hover:bg-accent/40',
          checked && 'bg-primary/5'
        )}
      >
        <Checkbox
          checked={checked}
          onCheckedChange={onToggle}
          className='mt-0.5'
          aria-label={t(m.selectRow, { host: host.host })}
        />
        <div className='min-w-0 flex-1 space-y-1'>
          <p className='font-mono text-sm font-medium break-all'>{host.host}</p>
          {host.samples.map((s) => (
            <p
              key={s}
              className='truncate font-mono text-xs text-muted-foreground'
              title={s}
            >
              {s}
            </p>
          ))}
        </div>
        <span className='shrink-0 text-sm font-semibold tabular-nums'>
          {formatNumber(host.count)}
          <span className='ms-1 text-xs font-normal text-muted-foreground'>
            {m.count}
          </span>
        </span>
      </label>
    </li>
  )
}
