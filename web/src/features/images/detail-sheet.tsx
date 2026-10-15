import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { t } from '@/i18n'
import { CircleHelp, Clock, Copy, ScrollText, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { copyText } from '@/lib/clipboard'
import {
  adminThumbSrc,
  formatBytes,
  formatNumber,
  formatRelativeTime,
  formatTime,
} from '@/lib/format'
import { failureText } from '@/lib/server-text'
import { cn } from '@/lib/utils'
import { useMessages } from '@/context/locale-provider'
import { useServerConfig } from '@/hooks/use-server-config'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { QueryState } from '@/components/query-state'
import { ImageBadges } from './badges'
import {
  copyFormats,
  deleteImage,
  deleteWarning,
  type ImageDetail,
  type ImagePatch,
  imageQueryOptions,
  imagesQueryKey,
  patchImage,
  type ReplicaItem,
  shareURL,
} from './data'

type ImageDetailSheetProps = {
  id: string | undefined // 当前打开的图片;undefined 为关闭
  isAdmin: boolean
  onClose: () => void
}

/** 图片详情抽屉:预览、分享链接、元数据、有效期 / 豁免管理、副本分布与删除。 */
export function ImageDetailSheet({
  id,
  isAdmin,
  onClose,
}: ImageDetailSheetProps) {
  // 关闭动画期间仍渲染上一张的内容,避免抽屉滑出时先变空白。
  const [shownId, setShownId] = useState(id)
  if (id !== undefined && id !== shownId) setShownId(id)

  return (
    <Sheet open={id !== undefined} onOpenChange={(o) => !o && onClose()}>
      <SheetContent className='w-full gap-0 p-0 sm:max-w-lg'>
        {shownId && (
          <DetailBody id={shownId} isAdmin={isAdmin} onClose={onClose} />
        )}
      </SheetContent>
    </Sheet>
  )
}

function DetailBody({
  id,
  isAdmin,
  onClose,
}: {
  id: string
  isAdmin: boolean
  onClose: () => void
}) {
  const m = useMessages()
  const config = useServerConfig()
  const queryClient = useQueryClient()
  const query = useQuery(imageQueryOptions(id))
  const [confirmDelete, setConfirmDelete] = useState(false)

  const patch = useMutation({
    mutationFn: (body: ImagePatch) => patchImage(id, body),
    onSuccess: (detail) => {
      queryClient.setQueryData(imageQueryOptions(id).queryKey, detail)
      queryClient.invalidateQueries({ queryKey: [...imagesQueryKey, 'list'] })
    },
  })
  const remove = useMutation({
    mutationFn: () => deleteImage(id),
    onSuccess: () => {
      toast.success(m.images.deleted)
      queryClient.invalidateQueries({ queryKey: imagesQueryKey })
      onClose()
    },
  })

  const image = query.data
  const url = image ? shareURL(image.url, config?.base_url) : ''

  return (
    <>
      <SheetHeader className='border-b'>
        <SheetTitle className='truncate pe-6'>
          {image?.file_name || (image ? m.images.untitled : id)}
        </SheetTitle>
        <SheetDescription>{m.images.detailTitle}</SheetDescription>
      </SheetHeader>
      <ScrollArea className='min-h-0 flex-1'>
        <div className='flex flex-col gap-5 p-4'>
          <QueryState query={query} skeleton={<DetailSkeleton />}>
            {image && (
              <>
                <a
                  href={url}
                  target='_blank'
                  rel='noreferrer'
                  title={m.images.openOriginal}
                  className='block overflow-hidden rounded-lg border bg-muted'
                >
                  <img
                    src={adminThumbSrc(image.id, config?.process_widths, 800)}
                    alt={image.file_name || image.id}
                    className='mx-auto max-h-80 w-full object-contain'
                  />
                </a>
                <ShareLinks url={url} widths={config?.process_widths} />
                <MetaList image={image} isAdmin={isAdmin} patch={patch} />
                <ReplicaList replicas={image.replicas} />
              </>
            )}
          </QueryState>
        </div>
      </ScrollArea>
      <SheetFooter className='flex-row items-center justify-between border-t'>
        <Button
          variant='destructive'
          disabled={!image || remove.isPending}
          onClick={() => setConfirmDelete(true)}
        >
          <Trash2 />
          {m.images.delete}
        </Button>
        {isAdmin && (
          <Button asChild variant='ghost' size='sm'>
            <Link to='/audit' search={{ target: id }}>
              <ScrollText />
              {m.images.auditLink}
            </Link>
          </Button>
        )}
      </SheetFooter>
      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={m.images.deleteTitle}
        desc={deleteWarning(m.images, config?.delete_grace_minutes)}
        confirmText={m.images.delete}
        destructive
        isLoading={remove.isPending}
        handleConfirm={() => remove.mutate()}
      />
    </>
  )
}

function DetailSkeleton() {
  return (
    <div className='space-y-4'>
      <Skeleton className='aspect-[4/3] w-full rounded-lg' />
      <Skeleton className='h-9 w-full' />
      {Array.from({ length: 6 }, (_, i) => (
        <Skeleton key={i} className='h-5 w-full' />
      ))}
    </div>
  )
}

function SectionTitle({ children }: { children: React.ReactNode }) {
  return (
    <h3 className='text-xs font-medium tracking-wide text-muted-foreground uppercase'>
      {children}
    </h3>
  )
}

/** 分享链接:完整地址、多格式复制与缩放档位。 */
function ShareLinks({
  url,
  widths,
}: {
  url: string
  widths: number[] | null | undefined
}) {
  const m = useMessages()
  const copy = async (text: string, done: string) => {
    if (await copyText(text)) toast.success(done)
  }
  return (
    <section className='space-y-2'>
      <SectionTitle>{m.images.shareLink}</SectionTitle>
      <div className='flex items-center gap-2'>
        <Input
          readOnly
          value={url}
          className='font-mono text-xs'
          onFocus={(e) => e.currentTarget.select()}
        />
        <Button
          variant='outline'
          size='icon'
          onClick={() => copy(url, m.images.linkCopied)}
        >
          <Copy />
          <span className='sr-only'>{m.images.copyLink}</span>
        </Button>
      </div>
      <div className='flex flex-wrap items-center gap-1.5'>
        <span className='text-xs text-muted-foreground'>{m.images.copyAs}</span>
        {copyFormats.map((f) => (
          <Button
            key={f.key}
            variant='outline'
            size='sm'
            className='h-7 px-2 text-xs'
            onClick={() =>
              copy(f.build(url), t(m.images.copiedAs, { format: f.label }))
            }
          >
            {f.label}
          </Button>
        ))}
      </div>
      {widths && widths.length > 0 && (
        <div className='flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground'>
          <span>{m.images.shareHint}</span>
          {widths.map((w) => (
            <button
              key={w}
              type='button'
              className='rounded-md border px-1.5 py-0.5 font-mono transition-colors hover:bg-accent hover:text-accent-foreground'
              onClick={() => copy(`${url}?w=${w}`, m.images.linkCopied)}
            >
              {w}
            </button>
          ))}
        </div>
      )}
    </section>
  )
}

function HelpTip({ text }: { text: string }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <CircleHelp className='size-3.5 text-muted-foreground/70' />
      </TooltipTrigger>
      <TooltipContent className='max-w-64'>{text}</TooltipContent>
    </Tooltip>
  )
}

function Row({
  label,
  help,
  children,
}: {
  label: string
  help?: string
  children: React.ReactNode
}) {
  return (
    <>
      <dt className='flex items-center gap-1 text-muted-foreground'>
        {label}
        {help && <HelpTip text={help} />}
      </dt>
      <dd className='min-w-0 break-words'>{children}</dd>
    </>
  )
}

type PatchMutation = ReturnType<
  typeof useMutation<ImageDetail, Error, ImagePatch>
>

/** 元数据清单;到期与豁免两项内嵌修改控件。 */
function MetaList({
  image,
  isAdmin,
  patch,
}: {
  image: ImageDetail
  isAdmin: boolean
  patch: PatchMutation
}) {
  const m = useMessages()
  return (
    <dl className='grid grid-cols-[7.5rem_minmax(0,1fr)] gap-x-3 gap-y-2.5 text-sm'>
      <Row label={m.images.format}>{image.mime}</Row>
      <Row label={m.images.dimensions}>
        <span className='tabular-nums'>
          {image.width} × {image.height}
        </span>
      </Row>
      <Row label={m.images.size}>{formatBytes(image.size)}</Row>
      <Row label={m.images.owner}>
        <span className='font-mono text-xs'>{image.user_id}</span>
      </Row>
      <Row label={m.images.source}>
        {image.token_name ?? (
          <span className='text-muted-foreground'>{m.images.sourceNone}</span>
        )}
      </Row>
      <Row label={m.images.refCount} help={m.images.refCountHelp}>
        {image.ref_count}
      </Row>
      <Row label={m.images.uploadedAt}>{formatTime(image.created_at)}</Row>
      <Row label={m.images.lastAccess}>
        {image.last_access_at ? (
          formatTime(image.last_access_at)
        ) : (
          <span className='text-muted-foreground'>
            {m.images.neverAccessed}
          </span>
        )}
      </Row>
      <Row label={m.images.accessCount} help={m.images.accessCountHelp}>
        {formatNumber(image.access_count)}
      </Row>
      <Row label={m.images.expiresAt}>
        <div className='flex flex-wrap items-center gap-x-3 gap-y-1'>
          <span>
            {image.expire_at ? formatTime(image.expire_at) : m.images.permanent}
          </span>
          <ImageBadges image={image} />
          <TtlEditor image={image} patch={patch} />
        </div>
      </Row>
      <Row label={m.images.cleanupExempt} help={m.images.exemptHelp}>
        {isAdmin ? (
          <Switch
            checked={image.cleanup_exempt}
            disabled={patch.isPending}
            aria-label={m.images.cleanupExempt}
            onCheckedChange={(checked) =>
              patch.mutate(
                { cleanup_exempt: checked },
                {
                  onSuccess: () =>
                    toast.success(
                      checked ? m.images.exemptSet : m.images.exemptUnset
                    ),
                }
              )
            }
          />
        ) : image.cleanup_exempt ? (
          m.images.exemptOn
        ) : (
          <span className='text-muted-foreground'>{m.images.exemptOff}</span>
        )}
      </Row>
      <Row label={m.images.sha256}>
        <code className='block font-mono text-xs break-all text-muted-foreground select-all'>
          {image.sha256}
        </code>
      </Row>
    </dl>
  )
}

/** 有效期修改:延长 N 天(从现在起算)或改永久。 */
function TtlEditor({
  image,
  patch,
}: {
  image: ImageDetail
  patch: PatchMutation
}) {
  const m = useMessages()
  const [open, setOpen] = useState(false)
  const [days, setDays] = useState('7')
  const n = Number(days)
  const valid = Number.isInteger(n) && n >= 1 && n <= 36500
  const run = (body: ImagePatch, done: string) =>
    patch.mutate(body, {
      onSuccess: () => {
        toast.success(done)
        setOpen(false)
      },
    })

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant='link' size='sm' className='h-auto p-0 text-xs'>
          <Clock />
          {m.images.editTtl}
        </Button>
      </PopoverTrigger>
      <PopoverContent align='start' className='w-60 space-y-2 p-3'>
        <div className='flex items-center gap-2'>
          <Input
            type='number'
            min={1}
            step={1}
            value={days}
            onChange={(e) => setDays(e.target.value)}
            className='h-8'
            aria-label={m.images.days}
          />
          <span className='text-sm text-muted-foreground'>{m.images.days}</span>
          <Button
            size='sm'
            disabled={!valid || patch.isPending}
            onClick={() =>
              run({ ttl_hours: n * 24 }, t(m.images.ttlExtended, { days: n }))
            }
          >
            {m.images.extend}
          </Button>
        </div>
        <Button
          variant='outline'
          size='sm'
          className='w-full'
          disabled={image.expire_at === null || patch.isPending}
          onClick={() => run({ ttl_hours: 0 }, m.images.ttlPermanent)}
        >
          {m.images.setPermanent}
        </Button>
      </PopoverContent>
    </Popover>
  )
}

const replicaStateClass: Record<ReplicaItem['state'], string> = {
  done: 'border-success/40 bg-success/10 text-success',
  failed: 'border-destructive/40 bg-destructive/10 text-destructive',
  uploading: 'border-info/40 bg-info/10 text-info',
  pending: 'text-muted-foreground',
}

/** 副本分布:各渠道的状态、尝试次数与最近错误。 */
function ReplicaList({ replicas }: { replicas: ReplicaItem[] }) {
  const m = useMessages()
  return (
    <section className='space-y-2'>
      <SectionTitle>{m.images.replicas}</SectionTitle>
      <p className='text-xs text-muted-foreground'>{m.images.replicasHelp}</p>
      <ul className='divide-y rounded-lg border'>
        {replicas.map((r) => (
          <li key={r.channel_id} className='space-y-1 px-3 py-2 text-sm'>
            <div className='flex items-center justify-between gap-2'>
              <span className='truncate font-medium'>{r.channel_name}</span>
              <Badge
                variant='outline'
                className={cn('shrink-0', replicaStateClass[r.state])}
              >
                {m.images.replicaState[r.state]}
              </Badge>
            </div>
            <div className='flex flex-wrap gap-x-3 text-xs text-muted-foreground'>
              <span>{t(m.images.attempts, { count: r.attempts })}</span>
              <span>
                {t(m.images.updatedAt, {
                  time: formatRelativeTime(r.updated_at),
                })}
              </span>
            </div>
            {r.failure && (
              <p className='text-xs break-all text-destructive'>
                {t(m.images.lastError, { error: failureText(r.failure) })}
              </p>
            )}
          </li>
        ))}
      </ul>
    </section>
  )
}
