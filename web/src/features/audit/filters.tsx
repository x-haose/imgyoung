import { useState } from 'react'
import { X } from 'lucide-react'
import { useMessages } from '@/context/locale-provider'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { AUDIT_ACTIONS, type AuditAction } from './actions'
import { type AuditFilters } from './data'
import { DateRangePicker } from './date-range-picker'

/** Radix Select 不接受空字符串选项值,「全部」用哨兵值表示。 */
const anyAction = '*'

type AuditFilterBarProps = {
  filters: AuditFilters
  onChange: (patch: Partial<Omit<AuditFilters, 'page' | 'page_size'>>) => void
}

/** 筛选条:动作下拉即选即用,文本框回车或失焦生效,日期范围选完即生效。 */
export function AuditFilterBar({ filters, onChange }: AuditFilterBarProps) {
  const m = useMessages()
  const active = !!(
    filters.action ||
    filters.actor ||
    filters.target ||
    filters.from
  )
  return (
    <div className='flex flex-wrap items-center gap-2'>
      <Select
        value={filters.action ?? anyAction}
        onValueChange={(v) =>
          onChange({ action: v === anyAction ? undefined : (v as AuditAction) })
        }
      >
        <SelectTrigger className='w-44' aria-label={m.audit.action}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={anyAction}>{m.audit.allActions}</SelectItem>
          {AUDIT_ACTIONS.map((action) => (
            <SelectItem key={action} value={action}>
              {m.audit.actions[action]}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <TextFilter
        value={filters.actor ?? ''}
        placeholder={m.audit.actorPlaceholder}
        ariaLabel={m.audit.actor}
        className='w-40'
        onCommit={(actor) => onChange({ actor: actor || undefined })}
      />
      <TextFilter
        value={filters.target ?? ''}
        placeholder={m.audit.targetPlaceholder}
        ariaLabel={m.audit.target}
        className='w-64'
        onCommit={(target) => onChange({ target: target || undefined })}
      />
      <DateRangePicker
        from={filters.from}
        to={filters.to}
        placeholder={m.audit.anyTime}
        clearLabel={m.audit.clearRange}
        onChange={(range) => onChange({ from: range.from, to: range.to })}
      />
      {active && (
        <Button
          variant='ghost'
          size='sm'
          onClick={() =>
            onChange({
              action: undefined,
              actor: undefined,
              target: undefined,
              from: undefined,
              to: undefined,
            })
          }
        >
          {m.audit.clearFilters}
          <X />
        </Button>
      )}
    </div>
  )
}

type TextFilterProps = {
  value: string
  placeholder: string
  ariaLabel: string
  className?: string
  onCommit: (value: string) => void
}

/** 文本筛选:本地编辑,回车或失焦时提交去空白后的值;地址栏的值变化时同步显示。 */
function TextFilter({
  value,
  placeholder,
  ariaLabel,
  className,
  onCommit,
}: TextFilterProps) {
  const [draft, setDraft] = useState(value)
  const [synced, setSynced] = useState(value)
  if (synced !== value) {
    setSynced(value)
    setDraft(value)
  }
  const commit = () => {
    const next = draft.trim()
    if (next !== value) onCommit(next)
  }
  return (
    <Input
      value={draft}
      placeholder={placeholder}
      aria-label={ariaLabel}
      className={className}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') commit()
      }}
    />
  )
}
