import { useState } from 'react'
import { Check, ChevronsUpDown } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useMessages } from '@/context/locale-provider'
import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'

export type ComboboxOption = {
  value: string
  label: string
  hint?: string // 标签右侧的灰字，也参与搜索
}

/** 可搜索的单选下拉。value 为空即选中「anyLabel」那一项。 */
export function Combobox({
  value,
  onChange,
  options,
  anyLabel,
  placeholder,
  disabled,
  className,
}: {
  value: string
  onChange: (value: string) => void
  options: ComboboxOption[]
  anyLabel: string
  placeholder?: string // 搜索框的提示
  disabled?: boolean
  className?: string
}) {
  const [open, setOpen] = useState(false)
  const m = useMessages()
  const current = options.find((o) => o.value === value)
  const pick = (v: string) => {
    onChange(v)
    setOpen(false)
  }
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          type='button'
          variant='outline'
          role='combobox'
          disabled={disabled}
          className={cn('justify-between px-3 font-normal', className)}
        >
          <span className={cn('truncate', !value && 'text-muted-foreground')}>
            {value ? current?.label || value : anyLabel}
          </span>
          <ChevronsUpDown className='opacity-50' />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        className='w-(--radix-popover-trigger-width) min-w-64 p-0'
        align='start'
      >
        <Command>
          <CommandInput placeholder={placeholder} />
          <CommandList>
            <CommandEmpty>{m.common.noMatch}</CommandEmpty>
            <CommandItem value='*' onSelect={() => pick('')}>
              {anyLabel}
              <Check className={cn('ms-auto', value && 'opacity-0')} />
            </CommandItem>
            {options.map((o) => (
              <CommandItem
                key={o.value}
                value={`${o.label} ${o.hint ?? ''} ${o.value}`}
                onSelect={() => pick(o.value)}
              >
                <span className='truncate'>{o.label}</span>
                {o.hint && (
                  <span className='truncate font-mono text-xs text-muted-foreground'>
                    {o.hint}
                  </span>
                )}
                <Check
                  className={cn('ms-auto', o.value !== value && 'opacity-0')}
                />
              </CommandItem>
            ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
