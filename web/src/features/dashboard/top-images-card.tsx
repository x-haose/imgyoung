import { t } from '@/i18n'
import { adminThumbSrc, formatBytes, formatNumber } from '@/lib/format'
import { useMessages } from '@/context/locale-provider'
import { useServerConfig } from '@/hooks/use-server-config'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { type TopImage } from './data'

/** 缩略图目标宽度(像素),按服务端宽度档就近取。 */
const THUMB_WIDTH = 100

/** 热门图列表:缩略图走后台不计数通道,点击行在新窗口打开公开外链。 */
export function TopImagesCard({ images }: { images: TopImage[] }) {
  const m = useMessages()
  const config = useServerConfig()
  return (
    <Card>
      <CardHeader>
        <CardTitle>{m.dashboard.topImages}</CardTitle>
        <CardDescription>{m.dashboard.topImagesDesc}</CardDescription>
      </CardHeader>
      <CardContent>
        {images.length === 0 ? (
          <p className='py-6 text-center text-sm text-muted-foreground'>
            {m.dashboard.noTopImages}
          </p>
        ) : (
          <ol className='grid gap-x-8 gap-y-1 xl:grid-cols-2'>
            {images.map((img, i) => (
              <li key={img.id}>
                <a
                  href={img.url}
                  target='_blank'
                  rel='noreferrer'
                  className='flex items-center gap-3 rounded-md px-2 py-2 transition outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring'
                >
                  <span className='w-5 shrink-0 text-end font-mono text-xs text-muted-foreground'>
                    {i + 1}
                  </span>
                  <img
                    src={adminThumbSrc(
                      img.id,
                      config?.process_widths,
                      THUMB_WIDTH
                    )}
                    alt={img.file_name || img.id}
                    loading='lazy'
                    width={40}
                    height={40}
                    className='size-10 shrink-0 rounded-md bg-muted object-cover'
                  />
                  <span className='min-w-0 flex-1'>
                    <span className='block truncate text-sm'>
                      {img.file_name || img.id}
                    </span>
                    <span className='block text-xs text-muted-foreground'>
                      {formatBytes(img.size)}
                    </span>
                  </span>
                  <span className='shrink-0 text-sm font-medium tabular-nums'>
                    {t(m.dashboard.accessCount, {
                      count: formatNumber(img.access_count),
                    })}
                  </span>
                </a>
              </li>
            ))}
          </ol>
        )}
      </CardContent>
    </Card>
  )
}
