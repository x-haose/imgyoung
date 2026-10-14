import { useState } from 'react'
import { format } from 'date-fns'
import { CalendarIcon, X } from 'lucide-react'
import { type DateRange } from 'react-day-picker'
import { enUS, zhCN } from 'react-day-picker/locale'
import { cn } from '@/lib/utils'
import { useLocale } from '@/context/locale-provider'
import { Button } from '@/components/ui/button'
import { Calendar } from '@/components/ui/calendar'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'

const calendarLocales = { 'zh-CN': zhCN, en: enUS }

/** YYYY-MM-DD → 本地日期对象;空值回 undefined。 */
const toDate = (day?: string) =>
  day
    ? new Date(+day.slice(0, 4), +day.slice(5, 7) - 1, +day.slice(8, 10))
    : undefined

type DateRangePickerProps = {
  from?: string // 起始日,YYYY-MM-DD
  to?: string // 截止日(含),YYYY-MM-DD
  onChange: (range: { from?: string; to?: string }) => void
  placeholder: string
  clearLabel: string
}

/** 日期范围筛选:日历选起止日,选完即生效;只选一天表示单日。 */
export function DateRangePicker({
  from,
  to,
  onChange,
  placeholder,
  clearLabel,
}: DateRangePickerProps) {
  const { locale } = useLocale()
  const [open, setOpen] = useState(false)
  const selected: DateRange | undefined = from
    ? { from: toDate(from), to: toDate(to) }
    : undefined
  const label = from ? `${from} — ${to ?? from}` : placeholder

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          type='button'
          variant='outline'
          className={cn(
            'h-9 justify-start px-3 font-normal',
            !from && 'text-muted-foreground'
          )}
        >
          <CalendarIcon className='opacity-60' />
          <span className='font-mono text-sm'>{label}</span>
        </Button>
      </PopoverTrigger>
      <PopoverContent className='w-auto p-2' align='start'>
        <Calendar
          mode='range'
          locale={calendarLocales[locale]}
          selected={selected}
          defaultMonth={selected?.from}
          onSelect={(range) =>
            onChange({
              from: range?.from ? format(range.from, 'yyyy-MM-dd') : undefined,
              to: range?.to ? format(range.to, 'yyyy-MM-dd') : undefined,
            })
          }
        />
        {from && (
          <div className='flex justify-end border-t pt-2'>
            <Button
              type='button'
              variant='ghost'
              size='sm'
              onClick={() => {
                onChange({})
                setOpen(false)
              }}
            >
              <X />
              {clearLabel}
            </Button>
          </div>
        )}
      </PopoverContent>
    </Popover>
  )
}
