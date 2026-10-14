import { t } from '@/i18n'
import { Clock } from 'lucide-react'
import { formatRelativeTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import { useMessages } from '@/context/locale-provider'
import { Badge } from '@/components/ui/badge'
import { type ImageItem, isExpired } from './data'

/** 图片状态徽标:待清理、已豁免与到期;全部为空时不渲染。 */
export function ImageBadges({
  image,
  className,
}: {
  image: ImageItem
  className?: string
}) {
  const m = useMessages()
  const expired = isExpired(image.expire_at)
  if (!image.pending_cleanup && !image.cleanup_exempt && !image.expire_at) {
    return null
  }
  return (
    <div className={cn('flex flex-wrap items-center gap-1', className)}>
      {image.pending_cleanup && (
        <Badge
          variant='outline'
          className='border-warning/40 bg-warning/10 text-warning'
        >
          {m.images.pendingCleanup}
        </Badge>
      )}
      {image.cleanup_exempt && (
        <Badge
          variant='outline'
          className='border-info/40 bg-info/10 text-info'
        >
          {m.images.exempt}
        </Badge>
      )}
      {image.expire_at && (
        <Badge
          variant='outline'
          className={cn(
            'gap-1 font-normal',
            expired
              ? 'border-destructive/40 bg-destructive/10 text-destructive'
              : 'text-muted-foreground'
          )}
        >
          <Clock />
          {expired
            ? m.images.expired
            : t(m.images.expiresIn, {
                time: formatRelativeTime(image.expire_at),
              })}
        </Badge>
      )}
    </div>
  )
}
