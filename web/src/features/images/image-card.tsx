import { Check, Link2 } from 'lucide-react'
import { toast } from 'sonner'
import { copyText } from '@/lib/clipboard'
import { adminThumbSrc, formatBytes } from '@/lib/format'
import { cn } from '@/lib/utils'
import { useMessages } from '@/context/locale-provider'
import { useServerConfig } from '@/hooks/use-server-config'
import { Button } from '@/components/ui/button'
import { ImageBadges } from './badges'
import { type ImageItem, shareURL } from './data'

type ImageCardProps = {
  image: ImageItem
  selecting: boolean // 多选模式:点卡片=勾选,不开详情
  selected: boolean
  onOpen: (id: string) => void
  onToggle: (id: string) => void
}

/** 图片卡片:缩略图 + 悬停一键复制;多选模式下整卡变为勾选目标。 */
export function ImageCard({
  image,
  selecting,
  selected,
  onOpen,
  onToggle,
}: ImageCardProps) {
  const m = useMessages()
  const config = useServerConfig()
  const name = image.file_name || m.images.untitled

  const activate = () => (selecting ? onToggle(image.id) : onOpen(image.id))
  const copy = async (e: React.MouseEvent) => {
    e.stopPropagation()
    if (await copyText(shareURL(image.url, config?.base_url))) {
      toast.success(m.images.linkCopied)
    }
  }

  return (
    <div
      role='button'
      tabIndex={0}
      aria-pressed={selecting ? selected : undefined}
      onClick={activate}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          activate()
        }
      }}
      className={cn(
        'group relative flex flex-col overflow-hidden rounded-xl border bg-card text-card-foreground shadow-sm transition-[border-color,box-shadow,transform] outline-none hover:border-primary/50 hover:shadow-md focus-visible:ring-2 focus-visible:ring-ring',
        selecting && selected && 'border-primary ring-2 ring-primary/50'
      )}
    >
      <div className='relative aspect-[4/3] overflow-hidden bg-muted'>
        <img
          src={adminThumbSrc(image.id, config?.process_widths, 400)}
          alt={name}
          loading='lazy'
          className='size-full object-cover transition-transform duration-300 group-hover:scale-[1.03]'
        />
        {selecting ? (
          <span
            className={cn(
              'absolute start-2 top-2 flex size-6 items-center justify-center rounded-full border-2 shadow-sm transition-colors',
              selected
                ? 'border-primary bg-primary text-primary-foreground'
                : 'border-white/80 bg-black/30 text-transparent'
            )}
          >
            <Check className='size-3.5' />
          </span>
        ) : (
          <div className='pointer-events-none absolute inset-x-0 bottom-0 flex justify-end bg-linear-to-t from-black/65 to-transparent p-2 pt-6 opacity-0 transition-opacity group-focus-within:pointer-events-auto group-focus-within:opacity-100 group-hover:pointer-events-auto group-hover:opacity-100'>
            <Button
              size='sm'
              variant='secondary'
              className='h-7 bg-white/90 text-neutral-900 hover:bg-white'
              onClick={copy}
            >
              <Link2 />
              {m.common.copy}
            </Button>
          </div>
        )}
      </div>
      <div className='flex flex-col gap-1.5 px-3 py-2.5'>
        <span className='truncate text-sm font-medium' title={name}>
          {name}
        </span>
        <div className='flex min-h-5 flex-wrap items-center justify-between gap-x-2 gap-y-1 text-xs text-muted-foreground'>
          <span className='shrink-0 tabular-nums'>
            {formatBytes(image.size)}
          </span>
          <ImageBadges image={image} className='justify-end' />
        </div>
      </div>
    </div>
  )
}
