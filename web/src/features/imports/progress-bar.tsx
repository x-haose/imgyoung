import { cn } from '@/lib/utils'

/** 进度条的一段:占总量的 value 份,className 决定颜色。 */
export type ProgressSegment = { value: number; className: string }

type ProgressBarProps = {
  total: number // 总量;0 时只有空轨道
  segments: ProgressSegment[] // 按顺序从左排列,合计不超过 total
  indeterminate?: boolean // 总量未知,整条脉动
  label: string // 读屏文案
  className?: string
}

/** 分段进度条:各段按占比排布。 */
export function ProgressBar({
  total,
  segments,
  indeterminate = false,
  label,
  className,
}: ProgressBarProps) {
  const sum = segments.reduce((acc, s) => acc + s.value, 0)
  return (
    <div
      role='progressbar'
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={total > 0 ? total : undefined}
      aria-valuenow={total > 0 ? sum : undefined}
      className={cn(
        'flex h-2 w-full overflow-hidden rounded-full bg-muted',
        indeterminate && 'animate-pulse bg-primary/20',
        className
      )}
    >
      {total > 0 &&
        segments.map((s, i) => (
          <div
            key={i}
            className={cn(
              'h-full transition-[width] duration-500',
              s.className
            )}
            style={{ width: `${(s.value / total) * 100}%` }}
          />
        ))}
    </div>
  )
}
