import { cn } from '@/lib/utils'
import { useMessages } from '@/context/locale-provider'
import { Input } from '@/components/ui/input'
import { type OptionsDraft } from './upload-options'

type UploadOptionsFieldsProps = {
  draft: OptionsDraft
  onChange: (draft: OptionsDraft) => void
  footnote?: string // 追加在两条档位说明之后的一句话
}

/** 有效期与 EXIF 档位的分段选择区,上传与批量导入共用。 */
export function UploadOptionsFields({
  draft,
  onChange,
  footnote,
}: UploadOptionsFieldsProps) {
  const m = useMessages()
  const ttlHint =
    draft.ttlMode === 'default'
      ? m.images.ttlDefaultHint
      : draft.ttlMode === 'never'
        ? m.images.ttlNeverHint
        : m.images.ttlCustomHint
  const hints = [ttlHint, m.images.exifHint]
  if (footnote) hints.push(footnote)

  return (
    <div className='grid gap-3 rounded-lg border bg-muted/30 p-3'>
      <div className='grid grid-cols-[auto_1fr] items-center gap-x-3 gap-y-2'>
        <span className='text-sm whitespace-nowrap'>{m.images.ttl}</span>
        <div className='flex flex-wrap items-center gap-x-3 gap-y-2'>
          <Segmented
            value={draft.ttlMode}
            onChange={(ttlMode) => onChange({ ...draft, ttlMode })}
            options={[
              { value: 'default', label: m.images.ttlDefault },
              { value: 'never', label: m.images.ttlNever },
              { value: 'custom', label: m.images.ttlCustom },
            ]}
          />
          {draft.ttlMode === 'custom' && (
            <div className='flex items-center gap-1.5'>
              <Input
                type='number'
                min={1}
                step={1}
                value={draft.ttlHours}
                onChange={(e) =>
                  onChange({ ...draft, ttlHours: e.target.value })
                }
                className='h-8 w-24'
                aria-label={m.images.hours}
              />
              <span className='text-xs text-muted-foreground'>
                {m.images.hours}
              </span>
            </div>
          )}
        </div>
        <span className='text-sm whitespace-nowrap'>{m.images.exifPolicy}</span>
        <div>
          <Segmented
            value={draft.exif}
            onChange={(exif) => onChange({ ...draft, exif })}
            options={[
              { value: 'default', label: m.images.exifDefault },
              { value: 'strip', label: m.images.exifStrip },
              { value: 'passthrough', label: m.images.exifPassthrough },
            ]}
          />
        </div>
      </div>
      <p className='text-xs text-muted-foreground'>
        {hints.join(m.common.sentenceSeparator)}
      </p>
    </div>
  )
}

/** 分段单选:一组互斥档位。 */
function Segmented<T extends string>({
  value,
  options,
  onChange,
}: {
  value: T
  options: { value: T; label: string }[]
  onChange: (value: T) => void
}) {
  return (
    <div
      role='radiogroup'
      className='inline-flex rounded-lg border bg-background p-0.5'
    >
      {options.map((o) => (
        <button
          key={o.value}
          type='button'
          role='radio'
          aria-checked={o.value === value}
          onClick={() => onChange(o.value)}
          className={cn(
            'rounded-md px-2.5 py-1 text-xs font-medium transition-colors',
            o.value === value
              ? 'bg-primary/15 text-primary'
              : 'text-muted-foreground hover:text-foreground'
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}
