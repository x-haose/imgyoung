import { createContext, useContext, useEffect, useMemo, useState } from 'react'
import { getCookie, setCookie } from '@/lib/cookies'

type Theme = 'dark' | 'light' | 'system'
type ResolvedTheme = Exclude<Theme, 'system'>

const DEFAULT_THEME: Theme = 'dark'
const THEME_COOKIE = 'imgyoung-theme'
const THEME_COOKIE_MAX_AGE = 60 * 60 * 24 * 365

/** 两种主题的画布色,写进 theme-color 让移动端浏览器外框同色。 */
const THEME_COLORS: Record<ResolvedTheme, string> = {
  dark: '#090c0f',
  light: '#eceff3',
}

function readTheme(): Theme {
  const saved = getCookie(THEME_COOKIE)
  return saved === 'light' || saved === 'dark' || saved === 'system'
    ? saved
    : DEFAULT_THEME
}

function resolveTheme(theme: Theme): ResolvedTheme {
  if (theme !== 'system') return theme
  return window.matchMedia('(prefers-color-scheme: dark)').matches
    ? 'dark'
    : 'light'
}

function applyTheme(resolved: ResolvedTheme) {
  const root = document.documentElement
  root.classList.toggle('dark', resolved === 'dark')
  root.classList.toggle('light', resolved === 'light')
  document
    .querySelector("meta[name='theme-color']")
    ?.setAttribute('content', THEME_COLORS[resolved])
}

// 首屏在 React 渲染前按已存偏好设置主题类,浅色用户不会先看到暗色一帧。
applyTheme(resolveTheme(readTheme()))

type ThemeContextValue = {
  theme: Theme
  resolvedTheme: ResolvedTheme
  setTheme: (theme: Theme) => void
}

const ThemeContext = createContext<ThemeContextValue | null>(null)

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [theme, setThemeState] = useState<Theme>(readTheme)
  const resolvedTheme = useMemo(() => resolveTheme(theme), [theme])

  useEffect(() => {
    applyTheme(resolvedTheme)
    if (theme !== 'system') return
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = () => applyTheme(mediaQuery.matches ? 'dark' : 'light')
    mediaQuery.addEventListener('change', onChange)
    return () => mediaQuery.removeEventListener('change', onChange)
  }, [theme, resolvedTheme])

  const value = useMemo<ThemeContextValue>(
    () => ({
      theme,
      resolvedTheme,
      setTheme: (next) => {
        setCookie(THEME_COOKIE, next, THEME_COOKIE_MAX_AGE)
        setThemeState(next)
      },
    }),
    [theme, resolvedTheme]
  )

  return <ThemeContext value={value}>{children}</ThemeContext>
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext)
  if (!ctx) throw new Error('useTheme must be used within a ThemeProvider')
  return ctx
}
