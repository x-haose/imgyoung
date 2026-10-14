import { useState } from 'react'
import { format } from 'date-fns'
import { useQuery } from '@tanstack/react-query'
import { t } from '@/i18n'
import { CalendarIcon, Filter, X } from 'lucide-react'
import { type DateRange } from 'react-day-picker'
import { enUS, zhCN } from 'react-day-picker/locale'
import { cn } from '@/lib/utils'
import { useLocale } from '@/context/locale-provider'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Calendar } from '@/components/ui/calendar'
import { Label } from '@/components/ui/label'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { tokenOptionsQueryOptions, userOptionsQueryOptions } from './data'
import { type ImagesSearch } from './search'

/** 筛选维度的修改;值为 undefined 表示清除该维度。 */
export type FilterPatch = Partial<
  Pick<ImagesSearch, 'user' | 'token' | 'since' | 'until' | 'exempt'>
>

type FilterProps = {
  isAdmin: boolean // 管理员额外提供归属用户维度
  search: ImagesSearch
  onChange: (patch: FilterPatch) => void
}

/** Radix Select 不接受空串作为选项值,「全部」用哨兵值。 */
const ALL = '*'

function useFilterOptions(isAdmin: boolean, user: string | undefined) {
  const users = useQuery({ ...userOptionsQueryOptions(), enabled: isAdmin })
  const tokens = useQuery(tokenOptionsQueryOptions(user))
  return {
    users: users.data?.items ?? [],
    tokens: tokens.data?.items ?? [],
    userName: (id: string) =>
      users.data?.items.find((u) => u.id === id)?.name ?? id,
    tokenName: (id: number) =>
      tokens.data?.items.find((tk) => tk.id === id)?.name ?? `#${id}`,
  }
}

function activeCount(s: ImagesSearch): number {
  return (
    Number(Boolean(s.user)) +
    Number(s.token > 0) +
    Number(Boolean(s.since || s.until)) +
    Number(Boolean(s.exempt))
  )
}

/** 筛选面板入口:Popover 收纳用户 / Token / 时间范围 / 仅看豁免四个维度,按钮上标出已激活数。 */
export function ImageFilters({ isAdmin, search, onChange }: FilterProps) {
  const { m } = useLocale()
  const { users, tokens } = useFilterOptions(isAdmin, search.user)
  const count = activeCount(search)

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant='outline' className='relative'>
          <Filter />
          {m.images.filter}
          {count > 0 && (
            <Badge className='absolute -end-2 -top-2 h-5 min-w-5 rounded-full px-1.5'>
              {count}
            </Badge>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent align='end' className='flex w-80 flex-col gap-4'>
        {isAdmin && (
          <div className='space-y-1.5'>
            <Label className='text-xs text-muted-foreground'>
              {m.images.filterUser}
            </Label>
            <Select
              value={search.user ?? ALL}
              // 换用户后 Token 归属随之变化,旧 Token 筛选一并清除。
              onValueChange={(v) =>
                onChange({ user: v === ALL ? undefined : v, token: undefined })
              }
            >
              <SelectTrigger className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL}>{m.images.allUsers}</SelectItem>
                {users.map((u) => (
                  <SelectItem key={u.id} value={u.id}>
                    {u.name || u.id}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}
        <div className='space-y-1.5'>
          <Label className='text-xs text-muted-foreground'>
            {m.images.filterToken}
          </Label>
          <Select
            value={search.token > 0 ? String(search.token) : ALL}
            onValueChange={(v) =>
              onChange({ token: v === ALL ? undefined : Number(v) })
            }
          >
            <SelectTrigger className='w-full'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{m.images.allTokens}</SelectItem>
              {tokens.map((tk) => (
                <SelectItem key={tk.id} value={String(tk.id)}>
                  {tk.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className='space-y-1.5'>
          <Label className='text-xs text-muted-foreground'>
            {m.images.filterRange}
          </Label>
          <DateRangeField
            since={search.since}
            until={search.until}
            onChange={(since, until) => onChange({ since, until })}
          />
        </div>
        <div className='flex items-center justify-between gap-3'>
          <div className='space-y-0.5'>
            <Label htmlFor='images-exempt-only' className='text-sm'>
              {m.images.filterExempt}
            </Label>
            <p className='text-xs text-muted-foreground'>
              {m.images.filterExemptHint}
            </p>
          </div>
          <Switch
            id='images-exempt-only'
            checked={Boolean(search.exempt)}
            onCheckedChange={(checked) =>
              onChange({ exempt: checked ? true : undefined })
            }
          />
        </div>
      </PopoverContent>
    </Popover>
  )
}

/** 日期范围:日历选起止日期,点「应用」才生效;只记录本地日期,时刻由列表查询按日界展开。 */
function DateRangeField({
  since,
  until,
  onChange,
}: {
  since: string | undefined
  until: string | undefined
  onChange: (since: string | undefined, until: string | undefined) => void
}) {
  const { m, locale } = useLocale()
  const [open, setOpen] = useState(false)
  const [range, setRange] = useState<DateRange | undefined>()
  const toDay = (v: string | undefined) =>
    v ? new Date(`${v}T00:00`) : undefined
  const label =
    since || until ? `${since ?? '…'} ~ ${until ?? '…'}` : m.images.rangeAny

  return (
    <Popover
      open={open}
      onOpenChange={(o) => {
        if (o) setRange({ from: toDay(since), to: toDay(until) })
        setOpen(o)
      }}
    >
      <PopoverTrigger asChild>
        <Button
          type='button'
          variant='outline'
          className='w-full justify-start px-3 font-normal'
        >
          <CalendarIcon className='opacity-60' />
          <span className={cn(!since && !until && 'text-muted-foreground')}>
            {label}
          </span>
        </Button>
      </PopoverTrigger>
      <PopoverContent className='w-auto p-3' align='start'>
        <Calendar
          mode='range'
          locale={locale === 'zh-CN' ? zhCN : enUS}
          selected={range}
          onSelect={setRange}
          defaultMonth={range?.from}
          className='p-0'
        />
        <div className='mt-3 flex justify-end gap-2'>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            onClick={() => {
              onChange(undefined, undefined)
              setOpen(false)
            }}
          >
            {m.images.rangeClear}
          </Button>
          <Button
            type='button'
            size='sm'
            disabled={!range?.from}
            onClick={() => {
              const from = range?.from
              const to = range?.to ?? range?.from
              onChange(
                from ? format(from, 'yyyy-MM-dd') : undefined,
                to ? format(to, 'yyyy-MM-dd') : undefined
              )
              setOpen(false)
            }}
          >
            {m.images.rangeApply}
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}

/** 已激活筛选的可清除标签行;没有激活项时不渲染。 */
export function ActiveFilterChips({ isAdmin, search, onChange }: FilterProps) {
  const { m } = useLocale()
  const { userName, tokenName } = useFilterOptions(isAdmin, search.user)
  if (activeCount(search) === 0) return null

  const chips: { key: string; label: string; clear: FilterPatch }[] = []
  if (search.user) {
    chips.push({
      key: 'user',
      label: t(m.images.chipUser, { name: userName(search.user) }),
      clear: { user: undefined, token: undefined },
    })
  }
  if (search.token > 0) {
    chips.push({
      key: 'token',
      label: t(m.images.chipToken, { name: tokenName(search.token) }),
      clear: { token: undefined },
    })
  }
  if (search.since || search.until) {
    chips.push({
      key: 'range',
      label: `${search.since ?? '…'} ~ ${search.until ?? '…'}`,
      clear: { since: undefined, until: undefined },
    })
  }
  if (search.exempt) {
    chips.push({
      key: 'exempt',
      label: m.images.chipExempt,
      clear: { exempt: undefined },
    })
  }

  return (
    <div className='flex flex-wrap items-center gap-2'>
      {chips.map((chip) => (
        <Badge key={chip.key} variant='secondary' className='gap-1 pe-1'>
          {chip.label}
          <button
            type='button'
            className='rounded-sm opacity-60 transition-opacity hover:opacity-100'
            aria-label={m.common.delete}
            onClick={() => onChange(chip.clear)}
          >
            <X className='size-3' />
          </button>
        </Badge>
      ))}
      <Button
        variant='ghost'
        size='sm'
        className='h-6 px-2 text-xs'
        onClick={() =>
          onChange({
            user: undefined,
            token: undefined,
            since: undefined,
            until: undefined,
            exempt: undefined,
          })
        }
      >
        {m.images.clearFilters}
      </Button>
    </div>
  )
}
