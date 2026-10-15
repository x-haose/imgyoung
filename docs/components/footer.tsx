import Link from 'next/link'

import type { Language } from '@/lib/i18n'
import { REPO_URL } from '@/lib/site'

/** 页脚文案。 */
const TEXT: Record<Language, Record<string, string>> = {
  zh: {
    tagline: '多渠道主备容灾的自部署图床。单二进制，Docker 一行起。',
    docs: '文档',
    quickStart: '快速开始',
    install: '安装',
    configuration: '配置',
    backup: '备份与恢复',
    developers: '开发者',
    library: 'Go 库',
    api: '接口参考',
    architecture: '架构',
    llmsIndex: '站点索引',
    llmsFull: '全文',
    project: '项目',
    releases: '发布版本',
    license: 'MIT 许可证',
  },
  en: {
    tagline:
      'Self-hosted image hosting with multi-channel failover. One binary, one Docker command.',
    docs: 'Docs',
    quickStart: 'Quick start',
    install: 'Install',
    configuration: 'Configuration',
    backup: 'Backup & restore',
    developers: 'Developers',
    library: 'Go library',
    api: 'API reference',
    architecture: 'Architecture',
    llmsIndex: 'Site index',
    llmsFull: 'Full text',
    project: 'Project',
    releases: 'Releases',
    license: 'MIT license',
  },
}

const linkClass = 'text-sm text-fd-muted-foreground transition-colors hover:text-fd-foreground'

/** 页脚的一列:标题加若干链接。 */
function Column({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className='flex flex-col gap-2.5'>
      <h2 className='text-xs font-medium tracking-wide text-fd-foreground'>{title}</h2>
      {children}
    </div>
  )
}

/**
 * 全站页脚。
 *
 * 「开发者」一列带上 llms.txt 端点——它们不在任何页面正文里,不列出来读者不会知道。
 * `className` 供调用方交代落位:文档版式是 CSS 网格,页脚要显式横跨整行。
 */
export function Footer({ lang, className }: { lang: string; className?: string }) {
  const t = TEXT[lang as Language] ?? TEXT.en
  const doc = (path: string) => `/${lang}/docs/${path}`

  return (
    <footer className={`relative ${className ?? ''}`}>
      {/* 分隔线从侧栏右缘画起:文档版式里侧栏是悬浮的,横贯整行的边框会从侧栏中间穿过 */}
      <div
        aria-hidden
        className='absolute inset-x-0 top-0 ms-[var(--fd-sidebar-width,0px)] border-t border-fd-border'
      />
      <div className='mx-auto grid w-full max-w-5xl gap-10 px-6 py-14 sm:grid-cols-2 lg:grid-cols-4'>
        <div className='flex flex-col gap-2.5'>
          <span className='text-sm font-semibold'>ImgYoung</span>
          <p className='text-sm leading-relaxed text-fd-muted-foreground'>{t.tagline}</p>
        </div>

        <Column title={t.docs}>
          <Link className={linkClass} href={doc('quick-start')}>
            {t.quickStart}
          </Link>
          <Link className={linkClass} href={doc('install')}>
            {t.install}
          </Link>
          <Link className={linkClass} href={doc('configuration')}>
            {t.configuration}
          </Link>
          <Link className={linkClass} href={doc('operations/backup')}>
            {t.backup}
          </Link>
        </Column>

        <Column title={t.developers}>
          <Link className={linkClass} href={doc('library')}>
            {t.library}
          </Link>
          <Link className={linkClass} href={doc('api')}>
            {t.api}
          </Link>
          <Link className={linkClass} href={doc('architecture')}>
            {t.architecture}
          </Link>
          <a className={`${linkClass} font-mono`} href={`/${lang}/llms.txt`}>
            llms.txt <span className='font-sans'>· {t.llmsIndex}</span>
          </a>
          <a className={`${linkClass} font-mono`} href={`/${lang}/llms-full.txt`}>
            llms-full.txt <span className='font-sans'>· {t.llmsFull}</span>
          </a>
        </Column>

        <Column title={t.project}>
          <a className={linkClass} href={REPO_URL} target='_blank' rel='noreferrer'>
            GitHub
          </a>
          <a className={linkClass} href={`${REPO_URL}/releases`} target='_blank' rel='noreferrer'>
            {t.releases}
          </a>
          <a
            className={linkClass}
            href={`${REPO_URL}/blob/main/LICENSE`}
            target='_blank'
            rel='noreferrer'
          >
            {t.license}
          </a>
        </Column>
      </div>
    </footer>
  )
}
