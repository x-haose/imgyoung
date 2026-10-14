import { Link, type LinkProps } from '@tanstack/react-router'
import {
  ArrowRight,
  BookOpen,
  CloudUpload,
  HardDrive,
  KeyRound,
  Rocket,
} from 'lucide-react'
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

type Step = {
  title: string
  desc: string
  to: LinkProps['to']
  icon: React.ElementType
}

/** 空站引导:三步接入;渠道一步仅管理员可进,普通用户只见后两步。 */
export function QuickStart({ admin }: { admin: boolean }) {
  const m = useMessages()
  const steps: Step[] = [
    ...(admin
      ? [
          {
            title: m.dashboard.step1Title,
            desc: m.dashboard.step1Desc,
            to: '/channels' as const,
            icon: HardDrive,
          },
        ]
      : []),
    {
      title: m.dashboard.step2Title,
      desc: m.dashboard.step2Desc,
      to: '/tokens',
      icon: KeyRound,
    },
    {
      title: m.dashboard.step3Title,
      desc: m.dashboard.step3Desc,
      to: '/guide',
      icon: CloudUpload,
    },
  ]
  return (
    <Card className='border-primary/30 bg-[linear-gradient(135deg,color-mix(in_oklab,var(--primary)_10%,var(--card)),var(--card)_55%)]'>
      <CardHeader>
        <CardTitle className='flex items-center gap-2'>
          <Rocket className='size-4 text-primary' />
          {m.dashboard.quickStartTitle}
        </CardTitle>
        <CardDescription>{m.dashboard.quickStartDesc}</CardDescription>
      </CardHeader>
      <CardContent className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3'>
        {steps.map((step, i) => (
          <Link
            key={step.to}
            to={step.to}
            className='group flex flex-col gap-3 rounded-lg border bg-background/60 p-4 transition outline-none hover:border-primary/60 hover:bg-background focus-visible:ring-2 focus-visible:ring-ring'
          >
            <div className='flex items-center gap-3'>
              <span className='flex size-7 items-center justify-center rounded-full bg-primary font-mono text-xs font-semibold text-primary-foreground'>
                {i + 1}
              </span>
              <step.icon className='size-4 text-primary' />
              <span className='font-medium'>{step.title}</span>
              <ArrowRight className='ms-auto size-4 text-muted-foreground transition group-hover:translate-x-0.5 group-hover:text-primary' />
            </div>
            <p className='text-sm leading-relaxed text-muted-foreground'>
              {step.desc}
            </p>
          </Link>
        ))}
      </CardContent>
      <CardFooter>
        <Button asChild variant='outline' size='sm'>
          <Link to='/guide'>
            <BookOpen />
            {m.dashboard.readGuide}
          </Link>
        </Button>
      </CardFooter>
    </Card>
  )
}
