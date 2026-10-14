import { Link, type LinkProps } from '@tanstack/react-router'
import { ArrowRight } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'

type SectionProps = {
  no: string // 两位序号,等宽字体
  icon: React.ElementType
  title: string
  action?: { to: LinkProps['to']; label: string } // 标题行右侧直达页面的入口
  className?: string
  children: React.ReactNode
}

/** 指南小节:序号 + 图标 + 标题的卡片。 */
export function Section({
  no,
  icon: Icon,
  title,
  action,
  className,
  children,
}: SectionProps) {
  return (
    <Card className={cn('min-w-0 gap-4', className)}>
      <CardHeader>
        <CardTitle className='flex items-center gap-2.5'>
          <span className='font-mono text-xs font-normal text-muted-foreground'>
            {no}
          </span>
          <Icon className='size-4 text-primary' />
          {title}
        </CardTitle>
        {action && (
          <CardAction>
            <Button asChild variant='ghost' size='sm'>
              <Link to={action.to}>
                {action.label}
                <ArrowRight />
              </Link>
            </Button>
          </CardAction>
        )}
      </CardHeader>
      <CardContent className='flex flex-col gap-4'>{children}</CardContent>
    </Card>
  )
}

/** 键值清单:左列字段名(等宽),右列说明;窄屏上下堆叠。 */
export function KeyValueList({
  items,
}: {
  items: { key: string; value: React.ReactNode }[]
}) {
  return (
    <dl className='divide-y rounded-lg border text-sm'>
      {items.map((item) => (
        <div
          key={item.key}
          className='grid gap-1 px-3 py-2.5 sm:grid-cols-[11rem_1fr] sm:gap-4'
        >
          <dt className='font-mono text-xs leading-5 text-muted-foreground'>
            {item.key}
          </dt>
          <dd className='min-w-0 leading-relaxed'>{item.value}</dd>
        </div>
      ))}
    </dl>
  )
}

/** 小节内的说明段落。 */
export function Note({ children }: { children: React.ReactNode }) {
  return (
    <p className='text-sm leading-relaxed text-muted-foreground'>{children}</p>
  )
}
