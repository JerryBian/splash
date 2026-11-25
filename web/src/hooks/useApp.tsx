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
import { api, setUnauthorizedHandler } from '../api/client'
import type { Bootstrap, Category, Overview, Project, Session } from '../api/types'
import { localToday } from '../lib/date'
import { describeError } from '../lib/errors'
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
  theme: ThemeChoice
  setTheme: (choice: ThemeChoice) => void
  /** Re-reads projects, categories and the overview after a mutation. */
  refresh: () => Promise<void>
  /** Signs out and returns to the login gate. */
  logout: () => Promise<void>
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

const noop = () => {}

/**
 * The signed-in app store. Everything here belongs to the administrator — the
 * login gate lives above this provider, so there is no anonymous branch. When
 * any request answers 401 (the idle session expired), onSignedOut tears the
 * app down back to the gate rather than stranding the user on dead screens.
 * onSignedOut defaults to a noop so isolated component tests can render the
 * provider without wiring the gate.
 */
export function AppProvider({
  initial,
  onSignedOut = noop,
  children,
}: {
  initial: Bootstrap
  onSignedOut?: () => void
  children: ReactNode
}) {
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

  // A 401 anywhere (except the auth door itself) means the session expired:
  // return to the gate. Registered for the life of the signed-in app.
  useEffect(() => {
    setUnauthorizedHandler(onSignedOut)
    return () => setUnauthorizedHandler(null)
  }, [onSignedOut])

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
      // The unauthorized interceptor already handles an expired session; any
      // other failure is surfaced as a toast without losing the current view.
      toast(describeError(error), 'error')
    }
  }, [toast])

  const logout = useCallback(async () => {
    setSession({ role: 'anonymous' })
    try {
      await api.logout()
    } finally {
      onSignedOut()
    }
  }, [onSignedOut])

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
      theme,
      setTheme,
      refresh,
      logout,
      toasts,
      toast,
      dismissToast,
    }),
    [session, projects, categories, overview, today, theme, setTheme, refresh, logout, toasts, toast, dismissToast],
  )

  return <AppContext.Provider value={value}>{children}</AppContext.Provider>
}
