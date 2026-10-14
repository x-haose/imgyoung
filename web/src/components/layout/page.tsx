import { cn } from '@/lib/utils'
import { Main } from '@/components/layout/main'

type PageProps = {
  title: string
  description?: React.ReactNode
  actions?: React.ReactNode // 标题行右侧的操作按钮
  fixed?: boolean // 内容区限高到视口、自己滚动(大列表用)
  className?: string
  children: React.ReactNode
}

/** 页面骨架:标题行(标题、说明、操作)+ 内容,各页观感一致。 */
export function Page({
  title,
  description,
  actions,
  fixed,
  className,
  children,
}: PageProps) {
  return (
    <Main fixed={fixed} className={cn('flex flex-col gap-6', className)}>
      <div className='flex flex-wrap items-start justify-between gap-4'>
        <div className='space-y-1'>
          <h1 className='text-2xl font-semibold tracking-tight'>{title}</h1>
          {description && (
            <p className='text-sm text-muted-foreground'>{description}</p>
          )}
        </div>
        {actions && <div className='flex items-center gap-2'>{actions}</div>}
      </div>
      {children}
    </Main>
  )
}
