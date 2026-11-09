import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { zh } from './zh'

export type Lang = 'en' | 'zh'
export type ThemeMode = 'light' | 'dark' | 'system'

type Vars = Record<string, string | number>

interface Settings {
  lang: Lang
  setLang: (l: Lang) => void
  themeMode: ThemeMode
  setThemeMode: (m: ThemeMode) => void
  dark: boolean
  /** Translate. English text is the key; `{name}` placeholders are filled from vars. */
  t: (text: string, vars?: Vars) => string
}

const Ctx = createContext<Settings | null>(null)

const fill = (s: string, vars?: Vars) =>
  vars ? s.replace(/\{(\w+)\}/g, (_, k) => (vars[k] === undefined ? `{${k}}` : String(vars[k]))) : s

const stored = <T extends string>(key: string, allowed: T[], def: T): T => {
  const v = localStorage.getItem(key) as T | null
  return v && allowed.includes(v) ? v : def
}

export function SettingsProvider({ children, onLangChange }: { children: ReactNode; onLangChange?: (l: Lang) => void }) {
  const [lang, setLangState] = useState<Lang>(() => stored('middeck.lang', ['en', 'zh'], 'en'))
  const [themeMode, setThemeModeState] = useState<ThemeMode>(() => stored('middeck.theme', ['light', 'dark', 'system'], 'system'))
  const [systemDark, setSystemDark] = useState(() => window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? false)

  useEffect(() => {
    const mq = window.matchMedia?.('(prefers-color-scheme: dark)')
    if (!mq) return
    const on = (e: MediaQueryListEvent) => setSystemDark(e.matches)
    mq.addEventListener('change', on)
    return () => mq.removeEventListener('change', on)
  }, [])

  useEffect(() => {
    onLangChange?.(lang)
    document.documentElement.lang = lang === 'zh' ? 'zh-CN' : 'en'
  }, [lang, onLangChange])

  const setLang = useCallback((l: Lang) => {
    localStorage.setItem('middeck.lang', l)
    setLangState(l)
  }, [])
  const setThemeMode = useCallback((m: ThemeMode) => {
    localStorage.setItem('middeck.theme', m)
    setThemeModeState(m)
  }, [])

  const dark = themeMode === 'dark' || (themeMode === 'system' && systemDark)

  const t = useCallback((text: string, vars?: Vars) => fill(lang === 'zh' ? zh[text] ?? text : text, vars), [lang])

  const value = useMemo(() => ({ lang, setLang, themeMode, setThemeMode, dark, t }), [lang, setLang, themeMode, setThemeMode, dark, t])
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

export function useSettings() {
  const s = useContext(Ctx)
  if (!s) throw new Error('useSettings outside SettingsProvider')
  return s
}

export const useT = () => useSettings().t
