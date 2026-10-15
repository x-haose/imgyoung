import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { t } from '@/i18n'
import { ArrowRight, RefreshCw, ShieldCheck, TriangleAlert } from 'lucide-react'
import { errorMessage } from '@/lib/errors'
import { formatRelativeTime, formatTime } from '@/lib/format'
import { alertText } from '@/lib/server-text'
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
import { type AlertItem, openAlertsQueryOptions } from './data'

/** 未恢复告警:列出类型、对象、描述与开启时刻;没有告警时给安静的「一切正常」。 */
export function AlertsCard({ className }: { className?: string }) {
  const m = useMessages()
  const query = useQuery(openAlertsQueryOptions())
  return (
    <Card className={className}>
      <CardHeader>
        <CardTitle>{m.dashboard.alerts}</CardTitle>
        <CardDescription>{m.dashboard.alertsDesc}</CardDescription>
        <CardAction>
          <Button asChild variant='ghost' size='sm'>
            <Link to='/settings' search={{ tab: 'alerts' }}>
              {m.dashboard.alertHistory}
              <ArrowRight />
            </Link>
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent>
        {query.isPending && (
          <div className='space-y-3'>
            <Skeleton className='h-12 w-full' />
            <Skeleton className='h-12 w-full' />
          </div>
        )}
        {query.isError && (
          <div className='flex flex-col items-center gap-2 py-6 text-center'>
            <p className='text-sm font-medium'>{m.dashboard.alertsFailed}</p>
            <p className='text-xs text-muted-foreground'>
              {errorMessage(query.error)}
            </p>
            <Button variant='outline' size='sm' onClick={() => query.refetch()}>
              <RefreshCw />
              {m.common.retry}
            </Button>
          </div>
        )}
        {query.data && query.data.items.length === 0 && (
          <div className='flex flex-col items-center gap-2 py-6 text-center'>
            <span className='flex size-10 items-center justify-center rounded-full bg-success/15 text-success'>
              <ShieldCheck className='size-5' />
            </span>
            <p className='text-sm font-medium'>{m.dashboard.allClear}</p>
            <p className='text-xs text-muted-foreground'>
              {m.dashboard.allClearDesc}
            </p>
          </div>
        )}
        {query.data && query.data.items.length > 0 && (
          <ul className='flex flex-col divide-y'>
            {query.data.items.map((a) => (
              <AlertRow key={a.id} alert={a} />
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}

function AlertRow({ alert }: { alert: AlertItem }) {
  const m = useMessages()
  return (
    <li className='flex gap-3 py-3 first:pt-0 last:pb-0'>
      <TriangleAlert className='mt-0.5 size-4 shrink-0 text-destructive' />
      <div className='min-w-0 flex-1 space-y-1'>
        <div className='flex flex-wrap items-center gap-2'>
          <Badge variant='destructive'>
            {m.dashboard.alertKinds[alert.kind]}
          </Badge>
          {alert.target && (
            <span className='truncate font-mono text-xs text-muted-foreground'>
              {alert.target}
            </span>
          )}
        </div>
        <p className='text-sm leading-relaxed break-words'>
          {alertText(alert)}
        </p>
        <p
          className='text-xs text-muted-foreground'
          title={formatTime(alert.opened_at)}
        >
          {t(m.dashboard.openedAt, {
            time: formatRelativeTime(alert.opened_at),
          })}
        </p>
      </div>
    </li>
  )
}
