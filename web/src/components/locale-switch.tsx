import { localeNames, locales } from '@/i18n'
import { Check, Languages } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useLocale } from '@/context/locale-provider'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

export function LocaleSwitch() {
  const { locale, setLocale, m } = useLocale()
  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <Button variant='ghost' size='icon' className='scale-95 rounded-full'>
          <Languages className='size-[1.2rem]' />
          <span className='sr-only'>{m.common.language}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align='end'>
        {locales.map((item) => (
          <DropdownMenuItem key={item} onClick={() => setLocale(item)}>
            {localeNames[item]}
            <Check
              size={14}
              className={cn('ms-auto', locale !== item && 'hidden')}
            />
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
