import { useState } from 'react'
import { t } from '@/i18n'
import { ChevronDown } from 'lucide-react'
import { formatNumber } from '@/lib/format'
import { cn } from '@/lib/utils'
import { useMessages } from '@/context/locale-provider'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { failureText, type ImportScanReport } from './data'

/** 扫描报告:汇总数字加两张可展开的明细表(抓取失败的页面、无法抓取的引用)。 */
export function ScanReport({ report }: { report: ImportScanReport }) {
  const m = useMessages().imports.scan
  return (
    <Card>
      <CardHeader>
        <CardTitle>{m.title}</CardTitle>
      </CardHeader>
      <CardContent className='space-y-4'>
        <dl className='grid grid-cols-2 gap-4 sm:grid-cols-5'>
          <Stat label={m.pages} value={report.pages} />
          <Stat
            label={m.withoutImages}
            hint={m.withoutImagesHint}
            value={report.pages_without_images}
          />
          <Stat label={m.offsite} value={report.offsite_pages} />
          <Stat label={m.failedPages} value={report.failed_pages.length} warn />
          <Stat label={m.unfetchable} value={report.unfetchable.length} warn />
        </dl>
        {report.failed_pages.length > 0 && (
          <Details
            title={t(m.failedPagesTitle, { count: report.failed_pages.length })}
          >
            {report.failed_pages.map((p) => (
              <li key={p.source} className='space-y-0.5 py-2'>
                <p className='font-mono text-xs break-all'>{p.source}</p>
                <p className='text-xs text-destructive'>
                  {failureText(p.code, p.detail)}
                </p>
                {p.detail !== '' && (
                  <p className='text-xs break-all text-muted-foreground'>
                    {p.detail}
                  </p>
                )}
              </li>
            ))}
          </Details>
        )}
        {report.unfetchable.length > 0 && (
          <Details
            title={t(m.unfetchableTitle, { count: report.unfetchable.length })}
            hint={m.unfetchableHint}
          >
            {report.unfetchable.map((u, i) => (
              <li key={`${i}-${u.source}`} className='space-y-0.5 py-2'>
                <p className='font-mono text-xs break-all'>{u.reference}</p>
                <p className='text-xs break-all text-muted-foreground'>
                  {u.source}
                </p>
              </li>
            ))}
          </Details>
        )}
      </CardContent>
    </Card>
  )
}

/** 汇总里的一个数字;warn 表示大于 0 时需要留意。 */
function Stat({
  label,
  hint,
  value,
  warn = false,
}: {
  label: string
  hint?: string
  value: number
  warn?: boolean
}) {
  return (
    <div className='space-y-1' title={hint}>
      <dd
        className={cn(
          'text-2xl font-semibold tracking-tight tabular-nums',
          warn && value > 0 && 'text-warning'
        )}
      >
        {formatNumber(value)}
      </dd>
      <dt className='text-xs text-muted-foreground'>{label}</dt>
    </div>
  )
}

/** 默认折叠的明细列表;条数可能很多,展开后限高滚动。 */
function Details({
  title,
  hint,
  children,
}: {
  title: string
  hint?: string
  children: React.ReactNode
}) {
  const [open, setOpen] = useState(false)
  return (
    <Collapsible open={open} onOpenChange={setOpen} className='border-t pt-3'>
      <CollapsibleTrigger className='flex items-center gap-1.5 rounded-md text-sm font-medium outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50'>
        <ChevronDown
          className={cn('size-4 transition-transform', !open && '-rotate-90')}
        />
        {title}
      </CollapsibleTrigger>
      <CollapsibleContent className='space-y-2 pt-2'>
        {hint && <p className='text-xs text-muted-foreground'>{hint}</p>}
        <ul className='max-h-72 divide-y overflow-y-auto rounded-lg border bg-muted/20 px-3'>
          {children}
        </ul>
      </CollapsibleContent>
    </Collapsible>
  )
}
