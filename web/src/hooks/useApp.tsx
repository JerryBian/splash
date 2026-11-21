import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { api } from '../api/client'
import type { Bootstrap, Category, Overview, Project, Session } from '../api/types'
import { localToday } from '../lib/date'
import { describeError, isUnauthorized } from '../lib/errors'
import { applyTheme, loadTheme, saveTheme, watchSystemTheme, type ThemeChoice } from '../lib/theme'

export type ToastTone = 'ok' | 'error' | 'info'

export type Toast = {
  id: number
  message: string
  tone: ToastTone
}

type AppContextValue = {
  session: Session
  projects: Project[]
  categories: Category[]
  overview: Overview
  today: string
  isAdmin: boolean
  theme: ThemeChoice
  setTheme: (choice: ThemeChoice) => void
  /** Re-reads projects, categories and the overview after a mutation. */
  refresh: () => Promise<void>
  /** Re-reads the session, then the data (visibility may have changed). */
  refreshSession: () => Promise<void>
  toasts: Toast[]
  toast: (message: string, tone?: ToastTone) => void
  dismissToast: (id: number) => void
}

const AppContext = createContext<AppContextValue | null>(null)

/** Reads the app store. Outside the provider it is a programming error. */
export function useApp(): AppContextValue {
  const value = useContext(AppContext)
  if (value === null) {
    throw new Error('useApp must be used inside <AppProvider>')
  }
  return value
}

let toastSeq = 0

export function AppProvider({ initial, children }: { initial: Bootstrap; children: ReactNode }) {
  const [session, setSession] = useState<Session>(initial.session)
  const [projects, setProjects] = useState<Project[]>(initial.projects)
  const [categories, setCategories] = useState<Category[]>(initial.categories)
  const [overview, setOverview] = useState<Overview>(initial.overview)
  const [today, setToday] = useState<string>(initial.today)
  const [toasts, setToasts] = useState<Toast[]>([])
  const [theme, setThemeState] = useState<ThemeChoice>(loadTheme)
  const timers = useRef<number[]>([])

  const toast = useCallback((message: string, tone: ToastTone = 'info') => {
    const id = ++toastSeq
    setToasts((current) => [...current.slice(-2), { id, message, tone }])
    const timer = window.setTimeout(() => {
      setToasts((current) => current.filter((t) => t.id !== id))
    }, 3200)
    timers.current.push(timer)
  }, [])

  const dismissToast = useCallback((id: number) => {
    setToasts((current) => current.filter((t) => t.id !== id))
  }, [])

  useEffect(() => {
    const pending = timers.current
    return () => {
      for (const timer of pending) window.clearTimeout(timer)
    }
  }, [])

  const refresh = useCallback(async () => {
    const day = localToday()
    try {
      const [projectsResponse, cats, ov] = await Promise.all([
        api.listProjects({ date: day }),
        api.listCategories(),
        api.overview(day),
      ])
      setProjects(projectsResponse.projects)
      setToday(projectsResponse.today)
      setCategories(cats)
      setOverview(ov)
    } catch (error) {
      if (isUnauthorized(error)) {
        // The session expired mid-use: fall back to the anonymous view rather
        // than stranding the user on a screen of errors.
        setSession({ role: 'anonymous' })
        toast('登录已过期，已切换为访客视图', 'info')
        return
      }
      toast(describeError(error), 'error')
    }
  }, [toast])

  const refreshSession = useCallback(async () => {
    try {
      const next = await api.me()
      setSession(next)
    } catch {
      setSession({ role: 'anonymous' })
    }
    await refresh()
  }, [refresh])

  const setTheme = useCallback((choice: ThemeChoice) => {
    setThemeState(choice)
    saveTheme(choice)
    applyTheme(choice)
  }, [])

  // Apply the stored preference on mount and follow the OS while in system mode.
  useEffect(() => {
    applyTheme(theme)
    if (theme !== 'system') return
    return watchSystemTheme(() => applyTheme('system'))
  }, [theme])

  const value = useMemo<AppContextValue>(
    () => ({
      session,
      projects,
      categories,
      overview,
      today,
      isAdmin: session.role === 'admin',
      theme,
      setTheme,
      refresh,
      refreshSession,
      toasts,
      toast,
      dismissToast,
    }),
    [session, projects, categories, overview, today, theme, setTheme, refresh, refreshSession, toasts, toast, dismissToast],
  )

  return <AppContext.Provider value={value}>{children}</AppContext.Provider>
}
