import { ServerCodeBlock } from 'fumadocs-ui/components/codeblock.rsc'
import { Tab, Tabs } from 'fumadocs-ui/components/tabs'
import type { Metadata } from 'next'
import Link from 'next/link'

import { FailoverDemo } from '@/components/home/failover-demo'
import { FeatureCard } from '@/components/home/feature-card'
import type { Language } from '@/lib/i18n'
import { languageAlternates, SITE_URL } from '@/lib/site'

import { COPY } from './copy'

/** 取某语言的文案;路由段不在支持列表时按英文。 */
function copyFor(lang: string) {
  return COPY[lang as Language] ?? COPY.en
}

export async function generateMetadata({ params }: PageProps<'/[lang]'>): Promise<Metadata> {
  const { lang } = await params
  const t = copyFor(lang)
  return {
    title: { absolute: t.metaTitle },
    description: t.metaDescription,
    alternates: {
      canonical: `${SITE_URL}/${lang}`,
      languages: languageAlternates((l) => `${SITE_URL}/${l}`),
    },
    openGraph: {
      title: t.metaTitle,
      description: t.metaDescription,
      url: `${SITE_URL}/${lang}`,
      siteName: 'ImgYoung',
      type: 'website',
    },
  }
}

export default async function HomePage({ params }: PageProps<'/[lang]'>) {
  const { lang } = await params
  const t = copyFor(lang)
  return (
    <main className='flex flex-1 flex-col'>
      <Hero lang={lang} t={t} />
      <Features t={t} />
      <Backends lang={lang} t={t} />
      <Install lang={lang} t={t} />
      <section className='border-t border-fd-border'>
        <ul className='mx-auto flex w-full max-w-5xl flex-wrap items-center justify-center gap-x-8 gap-y-2 px-6 py-8 text-sm text-fd-muted-foreground'>
          {t.strip.map((item) => (
            <li key={item}>{item}</li>
          ))}
        </ul>
      </section>
    </main>
  )
}

type SectionProps = { lang: string; t: ReturnType<typeof copyFor> }

/** 首屏:标语、两个入口按钮,右侧是可交互的主备切换示意图。背景是纯 CSS 的品牌色光晕与网格。 */
function Hero({ lang, t }: SectionProps) {
  return (
    <section aria-labelledby='hero-heading' className='relative overflow-hidden'>
      <div
        aria-hidden
        className='pointer-events-none absolute inset-0 [background:radial-gradient(60rem_28rem_at_15%_-10%,color-mix(in_oklab,var(--color-brand)_16%,transparent),transparent_70%),radial-gradient(40rem_22rem_at_90%_10%,color-mix(in_oklab,var(--color-sky)_10%,transparent),transparent_70%)]'
      />
      <div
        aria-hidden
        className='pointer-events-none absolute inset-0 [background-image:linear-gradient(var(--color-fd-border)_1px,transparent_1px),linear-gradient(90deg,var(--color-fd-border)_1px,transparent_1px)] [mask-image:radial-gradient(ellipse_at_top,black,transparent_70%)] [background-size:48px_48px] opacity-40'
      />
      <div className='relative mx-auto grid w-full max-w-6xl items-center gap-12 px-6 py-20 lg:grid-cols-[1fr_1.05fr] lg:py-28'>
        <div className='flex flex-col gap-6'>
          <p className='text-xs font-medium tracking-[0.08em] text-fd-primary uppercase'>
            {t.eyebrow}
          </p>
          <h1
            id='hero-heading'
            className='text-4xl leading-[1.15] font-bold tracking-tight sm:text-5xl'
          >
            {t.headline}
          </h1>
          <p className='max-w-xl text-lg leading-relaxed text-fd-muted-foreground'>{t.lede}</p>
          <div className='mt-2 flex flex-wrap items-center gap-3'>
            <Link
              href={`/${lang}/docs/quick-start`}
              className='rounded-xl bg-fd-primary px-5 py-2.5 text-sm font-medium text-fd-primary-foreground transition-opacity hover:opacity-90'
            >
              {t.start}
            </Link>
            <Link
              href={`/${lang}/docs/install`}
              className='rounded-xl border border-fd-border bg-fd-background/60 px-5 py-2.5 text-sm font-medium transition-colors hover:bg-fd-accent'
            >
              {t.install}
            </Link>
          </div>
        </div>
        <div className='flex justify-center lg:justify-end'>
          <FailoverDemo text={t.demo} />
        </div>
      </div>
    </section>
  )
}

/** 功能卡片两列网格。 */
function Features({ t }: Pick<SectionProps, 't'>) {
  return (
    <section aria-labelledby='features-heading' className='border-t border-fd-border'>
      <div className='mx-auto w-full max-w-5xl px-6 py-20'>
        <h2 id='features-heading' className='text-2xl font-semibold tracking-tight'>
          {t.featuresTitle}
        </h2>
        <ul className='mt-8 grid gap-4 sm:grid-cols-2'>
          {t.features.map((f) => (
            <FeatureCard key={f.title} title={f.title}>
              {f.body}
            </FeatureCard>
          ))}
        </ul>
      </div>
    </section>
  )
}

/** 支持的存储渠道:模板名平铺成标签,末尾一项说明其余 rclone 后端走自定义模式。 */
function Backends({ lang, t }: SectionProps) {
  return (
    <section aria-labelledby='backends-heading' className='border-t border-fd-border'>
      <div className='mx-auto grid w-full max-w-5xl gap-8 px-6 py-20 md:grid-cols-[1fr_1.4fr]'>
        <div className='flex flex-col gap-3'>
          <h2 id='backends-heading' className='text-2xl font-semibold tracking-tight'>
            {t.backendsTitle}
          </h2>
          <p className='text-sm leading-relaxed text-fd-muted-foreground'>{t.backendsLede}</p>
          <Link
            href={`/${lang}/docs/channels`}
            className='text-sm font-medium underline-offset-4 hover:underline'
          >
            {t.backendsTitle} →
          </Link>
        </div>
        <ul className='flex flex-wrap content-start gap-2'>
          {t.backends.map((name) => (
            <li
              key={name}
              className='rounded-lg border border-fd-border bg-fd-card px-3 py-1.5 text-sm transition-colors hover:border-fd-primary/50'
            >
              {name}
            </li>
          ))}
          <li className='rounded-lg border border-dashed border-fd-border px-3 py-1.5 text-sm text-fd-muted-foreground'>
            {t.backendsMore}
          </li>
        </ul>
      </div>
    </section>
  )
}

/** 安装命令页签,服务端高亮,带复制按钮;代码块占满整行,一行命令不折行也放得下。 */
function Install({ lang, t }: SectionProps) {
  return (
    <section aria-labelledby='install-heading' className='border-t border-fd-border'>
      <div className='mx-auto flex w-full max-w-5xl flex-col gap-8 px-6 py-20'>
        <div className='flex max-w-2xl flex-col gap-3'>
          <h2 id='install-heading' className='text-2xl font-semibold tracking-tight'>
            {t.installTitle}
          </h2>
          <p className='text-sm leading-relaxed text-fd-muted-foreground'>{t.installLede}</p>
          <Link
            href={`/${lang}/docs/install`}
            className='text-sm font-medium underline-offset-4 hover:underline'
          >
            {t.installMore}
          </Link>
        </div>
        <Tabs items={t.installTabs.map((tab) => tab.label)}>
          {t.installTabs.map((tab) => (
            <Tab key={tab.label} value={tab.label}>
              <ServerCodeBlock code={tab.code} lang={tab.lang} />
            </Tab>
          ))}
        </Tabs>
      </div>
    </section>
  )
}
