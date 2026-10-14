import { Link } from '@tanstack/react-router'
import { ArrowRight } from 'lucide-react'
import { formatBytes } from '@/lib/format'
import { cn } from '@/lib/utils'
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
import { type ChannelStat } from './data'

/** 条形的最小宽度(百分比):有数据的渠道哪怕占比极小也要看得见。 */
const MIN_BAR_PERCENT = 2

function sharePercent(bytes: number, total: number): number {
  if (bytes <= 0 || total <= 0) return 0
  return Math.max(MIN_BAR_PERCENT, Math.round((bytes / total) * 100))
}

/** 渠道健康与水位:每渠道一行,条形为其字节占全站的比例,不健康用警示色。 */
export function ChannelsCard({
  channels,
  className,
}: {
  channels: ChannelStat[]
  className?: string
}) {
  const m = useMessages()
  const total = channels.reduce((sum, c) => sum + c.bytes, 0)
  return (
    <Card className={className}>
      <CardHeader>
        <CardTitle>{m.dashboard.channels}</CardTitle>
        <CardDescription>{m.dashboard.channelsDesc}</CardDescription>
        <CardAction>
          <Button asChild variant='ghost' size='sm'>
            <Link to='/channels'>
              {m.dashboard.manageChannels}
              <ArrowRight />
            </Link>
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className='flex flex-col gap-4'>
        {channels.length === 0 && (
          <p className='py-6 text-center text-sm text-muted-foreground'>
            {m.dashboard.noChannels}
          </p>
        )}
        {channels.map((c) => (
          <div key={c.id} className='space-y-2'>
            <div className='flex items-center gap-2 text-sm'>
              <span
                className={cn(
                  'size-2 shrink-0 rounded-full',
                  c.healthy ? 'bg-success' : 'bg-destructive'
                )}
              />
              <span className='truncate font-medium'>{c.name}</span>
              <Badge
                variant='outline'
                className='font-mono text-[11px] text-muted-foreground'
              >
                {c.driver}
              </Badge>
              {!c.healthy && (
                <Badge variant='destructive'>{m.dashboard.unhealthy}</Badge>
              )}
              <span className='ms-auto shrink-0 text-muted-foreground tabular-nums'>
                {formatBytes(c.bytes)}
              </span>
            </div>
            <div className='h-1.5 w-full overflow-hidden rounded-full bg-muted'>
              <div
                className={cn(
                  'h-full rounded-full transition-[width]',
                  c.healthy ? 'bg-primary' : 'bg-destructive'
                )}
                style={{ width: `${sharePercent(c.bytes, total)}%` }}
              />
            </div>
          </div>
        ))}
      </CardContent>
    </Card>
  )
}
