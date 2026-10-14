import { useState } from 'react'
import { t } from '@/i18n'
import { ExternalLink } from 'lucide-react'
import { adminThumbSrc, formatBytes, formatRelativeTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import { useMessages } from '@/context/locale-provider'
import { useServerConfig } from '@/hooks/use-server-config'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { type CandidateItem, type CandidateSide } from './data'

type SidePanelProps = {
  side: CandidateSide
  label: string
  onKeep: () => void
  disabled: boolean
}

/** 候选对的一方:预览、元数据与「保留这张」。 */
function SidePanel({ side, label, onKeep, disabled }: SidePanelProps) {
  const m = useMessages()
  const config = useServerConfig()
  return (
    <div className='flex min-w-0 flex-1 flex-col gap-2'>
      <div className='relative aspect-[4/3] overflow-hidden rounded-lg bg-muted'>
        {side.image_id ? (
          <>
            <img
              src={adminThumbSrc(side.image_id, config?.process_widths, 400)}
              alt={label}
              loading='lazy'
              className='size-full object-contain'
            />
            <a
              href={`/api/admin/images/${side.image_id}/thumb`}
              target='_blank'
              rel='noreferrer'
              aria-label={m.dedup.openOriginal}
              className='absolute end-2 top-2 rounded-md bg-background/80 p-1.5 text-muted-foreground opacity-0 backdrop-blur transition group-hover/card:opacity-100 hover:text-foreground focus-visible:opacity-100'
            >
              <ExternalLink className='size-4' />
            </a>
          </>
        ) : (
          <div className='flex size-full items-center justify-center text-sm text-muted-foreground'>
            {m.dedup.noPreview}
          </div>
        )}
      </div>
      <div className='flex items-center justify-between gap-2 text-xs text-muted-foreground'>
        <span className='truncate'>
          {t(m.dedup.meta, {
            mime: side.mime,
            width: side.width,
            height: side.height,
            size: formatBytes(side.size),
          })}
        </span>
        <span className='shrink-0 font-medium text-foreground'>
          {t(m.dedup.refs, { count: side.refs })}
        </span>
      </div>
      <Button
        variant='outline'
        className='w-full'
        onClick={onKeep}
        disabled={disabled}
      >
        {m.dedup.keepThis}
      </Button>
    </div>
  )
}

type CandidateCardProps = {
  candidate: CandidateItem
  selected: boolean
  onSelectedChange: (selected: boolean) => void
  onMerge: (keep: CandidateSide) => Promise<unknown>
  onDismiss: () => Promise<unknown>
  busy: boolean // 该候选正在提交裁决
}

/** 一组候选:左右对照,合并需先确认保留方,忽略直接执行。 */
export function CandidateCard({
  candidate,
  selected,
  onSelectedChange,
  onMerge,
  onDismiss,
  busy,
}: CandidateCardProps) {
  const m = useMessages()
  const [keep, setKeep] = useState<CandidateSide | null>(null)
  const other = keep && (keep === candidate.a ? candidate.b : candidate.a)
  const nearIdentical = candidate.distance <= 2

  return (
    <div
      className={cn(
        'group/card flex min-w-0 flex-col gap-4 rounded-xl border bg-card p-4 shadow-xs transition-colors',
        selected && 'border-primary/60 ring-1 ring-primary/30'
      )}
    >
      <div className='flex flex-wrap items-center gap-3'>
        <Checkbox
          checked={selected}
          onCheckedChange={(v) => onSelectedChange(v === true)}
          aria-label={t(m.dedup.selectCandidate, { id: candidate.id })}
        />
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge
              variant='outline'
              className={cn(
                'cursor-default',
                nearIdentical
                  ? 'border-destructive/40 bg-destructive/10 text-destructive'
                  : 'border-warning/40 bg-warning/10 text-warning'
              )}
            >
              {t(m.dedup.distance, { distance: candidate.distance })}
            </Badge>
          </TooltipTrigger>
          <TooltipContent>{m.dedup.distanceHint}</TooltipContent>
        </Tooltip>
        <span className='text-sm font-medium'>
          {t(m.dedup.candidate, { id: candidate.id })}
        </span>
        <span className='text-xs text-muted-foreground'>
          {t(m.dedup.foundAt, {
            time: formatRelativeTime(candidate.created_at),
          })}
        </span>
        <Button
          variant='ghost'
          size='sm'
          className='ms-auto'
          disabled={busy}
          onClick={() => void onDismiss()}
        >
          {m.dedup.dismiss}
        </Button>
      </div>
      <div className='flex gap-4'>
        <SidePanel
          side={candidate.a}
          label={m.dedup.sideA}
          disabled={busy}
          onKeep={() => setKeep(candidate.a)}
        />
        <SidePanel
          side={candidate.b}
          label={m.dedup.sideB}
          disabled={busy}
          onKeep={() => setKeep(candidate.b)}
        />
      </div>
      <ConfirmDialog
        open={keep !== null}
        onOpenChange={(open) => !open && setKeep(null)}
        title={m.dedup.mergeTitle}
        desc={t(m.dedup.mergeDesc, { refs: other?.refs ?? 0 })}
        confirmText={m.dedup.merge}
        destructive
        isLoading={busy}
        handleConfirm={() => {
          if (!keep) return
          void onMerge(keep).finally(() => setKeep(null))
        }}
      />
    </div>
  )
}
