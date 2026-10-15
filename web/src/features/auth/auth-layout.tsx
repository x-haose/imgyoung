import { BrandMark } from '@/assets/brand-mark'
import { Card, CardContent } from '@/components/ui/card'
import { AuroraBackground } from '@/components/aurora-background'
import { LocaleSwitch } from '@/components/locale-switch'
import { ThemeSwitch } from '@/components/theme-switch'

type AuthLayoutProps = {
  title: string
  subtitle?: React.ReactNode
  children: React.ReactNode
}

/**
 * 未登录页面的外框:极光背景上居中单卡片,底部语言 / 主题切换。
 * 极光画不出来(无 WebGL2)时,下层的品牌色径向光晕即是背景。
 */
export function AuthLayout({ title, subtitle, children }: AuthLayoutProps) {
  return (
    <div className='relative flex min-h-svh flex-col items-center justify-center overflow-hidden bg-sidebar p-6'>
      <div
        aria-hidden='true'
        className='pointer-events-none absolute inset-x-0 top-0 h-[32rem] bg-[radial-gradient(44rem_22rem_at_50%_-6rem,color-mix(in_oklab,var(--primary)_22%,transparent),transparent_70%)]'
      />
      <AuroraBackground />
      <div className='relative w-full max-w-sm'>
        <div className='mb-6 flex flex-col items-center gap-3 text-center'>
          <BrandMark className='size-11' />
          <div>
            <h1 className='text-xl font-semibold tracking-tight'>{title}</h1>
            {subtitle && (
              <p className='mt-1 text-sm text-muted-foreground'>{subtitle}</p>
            )}
          </div>
        </div>
        <Card className='shadow-lg'>
          <CardContent>{children}</CardContent>
        </Card>
        <div className='mt-6 flex items-center justify-center gap-1'>
          <LocaleSwitch />
          <ThemeSwitch />
        </div>
      </div>
    </div>
  )
}
