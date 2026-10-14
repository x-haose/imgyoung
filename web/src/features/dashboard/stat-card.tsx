import { Link, type LinkProps } from '@tanstack/react-router'
import { cn } from '@/lib/utils'
import { Card, CardContent } from '@/components/ui/card'

type StatCardProps = {
  label: string
  value: string
  hint?: string
  icon: React.ElementType
  to?: LinkProps['to'] // 给定时整卡可点,直达处置页
  attention?: boolean // 数值需要处理时用警示色强调
}

/** 指标卡:标签、主数值、一行说明与右上角图标。 */
export function StatCard({
  label,
  value,
  hint,
  icon: Icon,
  to,
  attention,
}: StatCardProps) {
  const card = (
    <Card
      className={cn(
        'h-full gap-0 py-5',
        to &&
          'transition hover:-translate-y-px hover:border-primary/50 hover:shadow-md'
      )}
    >
      <CardContent className='flex items-start justify-between gap-4 px-5'>
        <div className='min-w-0 space-y-1'>
          <p className='text-sm text-muted-foreground'>{label}</p>
          <p
            className={cn(
              'text-2xl font-semibold tracking-tight tabular-nums',
              attention && 'text-warning'
            )}
          >
            {value}
          </p>
          {hint && (
            <p className='text-xs leading-relaxed text-muted-foreground'>
              {hint}
            </p>
          )}
        </div>
        <div
          className={cn(
            'flex size-9 shrink-0 items-center justify-center rounded-lg',
            attention
              ? 'bg-warning/15 text-warning'
              : 'bg-primary/10 text-primary'
          )}
        >
          <Icon className='size-4.5' />
        </div>
      </CardContent>
    </Card>
  )
  if (!to) return card
  return (
    <Link
      to={to}
      className='block rounded-xl outline-none focus-visible:ring-2 focus-visible:ring-ring'
    >
      {card}
    </Link>
  )
}
