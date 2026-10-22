import { t } from '@/i18n'
import { formatNumber, formatTime } from '@/lib/format'
import { useMessages } from '@/context/locale-provider'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { type ImportJob } from './data'

const SECONDS_PER_HOUR = 3600

type Row = { label: string; value: React.ReactNode }

/** 任务参数:输入类型、时间与创建时指定的有效期、EXIF、Referer;未指定的项说明取值来源。 */
export function SettingsCard({ job }: { job: ImportJob }) {
  const m = useMessages()
  const d = m.imports.detail
  const ttl =
    job.ttl_seconds === null
      ? d.ttlDefault
      : job.ttl_seconds === 0
        ? d.ttlNever
        : t(d.ttlHours, {
            hours: formatNumber(
              Math.round((job.ttl_seconds / SECONDS_PER_HOUR) * 10) / 10
            ),
          })
  const exif =
    job.exif_policy === 'strip'
      ? m.images.exifStrip
      : job.exif_policy === 'passthrough'
        ? m.images.exifPassthrough
        : d.exifDefault
  const rows: Row[] = [
    { label: d.mode, value: m.imports.mode[job.mode] },
    { label: d.created, value: formatTime(job.created_at) },
  ]
  if (job.finished_at) {
    rows.push({ label: d.finished, value: formatTime(job.finished_at) })
  }
  rows.push(
    { label: d.ttl, value: ttl },
    { label: d.exif, value: exif },
    { label: d.referer, value: job.referer || d.refererDefault }
  )
  if (job.seeds.length > 0) {
    rows.push({
      label: d.seeds,
      value: (
        <ul className='space-y-0.5 font-mono text-xs'>
          {job.seeds.map((s) => (
            <li key={s} className='break-all'>
              {s}
            </li>
          ))}
        </ul>
      ),
    })
  }
  return (
    <Card>
      <CardHeader>
        <CardTitle>{d.settingsTitle}</CardTitle>
      </CardHeader>
      <CardContent>
        <dl className='grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm'>
          {rows.map(({ label, value }) => (
            <div key={label} className='contents'>
              <dt className='text-muted-foreground'>{label}</dt>
              <dd className='min-w-0'>{value}</dd>
            </div>
          ))}
        </dl>
      </CardContent>
    </Card>
  )
}
