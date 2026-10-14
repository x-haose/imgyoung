import { createContext, useContext, useEffect, useMemo, useState } from 'react'
import {
  getLocale,
  loadLocale,
  type Locale,
  type Messages,
  messages,
  setCurrentLocale,
} from '@/i18n'

type LocaleContextValue = {
  locale: Locale
  setLocale: (locale: Locale) => void
  m: Messages
}

const LocaleContext = createContext<LocaleContextValue | null>(null)

export function LocaleProvider({ children }: { children: React.ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(getLocale)

  useEffect(() => {
    document.documentElement.lang = locale
    document.title = messages().common.appTitle
  }, [locale])

  const value = useMemo<LocaleContextValue>(
    () => ({
      locale,
      m: messages(),
      setLocale: (next) => {
        void loadLocale(next).then(() => {
          setCurrentLocale(next)
          setLocaleState(next)
        })
      },
    }),
    [locale]
  )

  return <LocaleContext value={value}>{children}</LocaleContext>
}

export function useLocale(): LocaleContextValue {
  const ctx = useContext(LocaleContext)
  if (!ctx) throw new Error('useLocale must be used within a LocaleProvider')
  return ctx
}

/** 当前语言的词典;语言切换时使用方随之重绘。 */
export function useMessages(): Messages {
  return useLocale().m
}
