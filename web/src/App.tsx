import { Suspense, use, useEffect, useState } from 'react'
import { loadBootstrap } from './api/client'
import type { Bootstrap } from './api/types'
import { BootSkeleton } from './components/BootSkeleton'
import { CalendarIcon, ChartIcon, LockIcon, SealMark, SlidersIcon, UserIcon } from './components/icons'
import { LoginView } from './components/LoginView'
import { ManageView } from './components/ManageView'
import { MeView } from './components/MeView'
import { ToastHost } from './components/overlays'
import { ProjectDetail } from './components/ProjectDetail'
import { ProjectEditor } from './components/ProjectEditor'
import { StatsView } from './components/StatsView'
import { TodayView } from './components/TodayView'
import { Button } from './components/ui'
import { AppProvider, useApp } from './hooks/useApp'
import { navigate, parseHash, useHashRoute, type Route } from './hooks/useNow'
import { describeError } from './lib/errors'

/**
 * Fetches the startup calls in parallel and never rejects.
 *
 * Returning the failure as a value rather than throwing it is what lets the
 * shell render a useful message when the Go server is not running, instead of
 * relying on an error boundary nobody has written yet.
 */
type BootstrapResult = { ok: true; data: Bootstrap } | { ok: false; error: unknown }

function startBootstrap(): Promise<BootstrapResult> {
  return loadBootstrap()
    .then((data: Bootstrap): BootstrapResult => ({ ok: true, data }))
    .catch((error: unknown): BootstrapResult => ({ ok: false, error }))
}

export function App() {
  // The promise is created once, in a state initializer. use() needs a promise
  // that is stable across renders — building it in the render body would suspend
  // again on every render and never resolve. Retrying replaces it with a new one.
  const [bootstrap, setBootstrap] = useState<Promise<BootstrapResult>>(startBootstrap)

  return (
    <div className="app-frame">
      <Suspense fallback={<BootSkeleton />}>
        <Shell promise={bootstrap} onRetry={() => setBootstrap(startBootstrap())} />
      </Suspense>
    </div>
  )
}

function Shell({ promise, onRetry }: { promise: Promise<BootstrapResult>; onRetry: () => void }) {
  const result = use(promise)
  if (!result.ok) {
    return <BootFailure error={result.error} onRetry={onRetry} />
  }
  return (
    <AppProvider initial={result.data}>
      <Router />
    </AppProvider>
  )
}

type Tab = 'today' | 'stats' | 'manage' | 'me'

function tabOf(route: Route): Tab | null {
  switch (route.name) {
    case 'today':
    case 'stats':
    case 'manage':
    case 'me':
      return route.name
    default:
      return null
  }
}

/**
 * Hash routing with a persistent base tab: overlays (project, editor, login)
 * stack over the tab they were opened from, and the mobile back button closes
 * them one at a time.
 */
function Router() {
  const route = useHashRoute()
  const { isAdmin } = useApp()
  const [tab, setTab] = useState<Tab>(() => tabOf(parseHash(window.location.hash)) ?? 'today')

  // Render-phase adjustment (the React docs' "derive state from props"
  // pattern): when the hash names a tab, the base tab follows immediately,
  // without an effect and without a stale frame.
  const routeTab = tabOf(route)
  if (routeTab !== null && routeTab !== tab) {
    setTab(routeTab)
  }

  useEffect(() => {
    if (tab) window.scrollTo(0, 0)
  }, [tab])

  return (
    <div className="app">
      <Sidebar tab={tab} isAdmin={isAdmin} />
      <div className="app-body">
        <TopBar />
        <main className="app-main">
          {tab === 'today' ? <TodayView /> : null}
          {tab === 'stats' ? <StatsView /> : null}
          {tab === 'manage' ? <ManageView /> : null}
          {tab === 'me' ? <MeView /> : null}
        </main>
        <BottomNav tab={tab} isAdmin={isAdmin} />
      </div>

      {route.name === 'project' ? (
        <div className="overlay-page">
          {/* Keyed so switching projects resets disclosure state and scroll
              instead of showing the previous project's expanded history. */}
          <ProjectDetail key={route.id} projectId={route.id} />
        </div>
      ) : null}
      {route.name === 'edit' ? (
        <div className="overlay-page">
          <ProjectEditor key={route.id} projectId={route.id} />
        </div>
      ) : null}
      {route.name === 'login' ? <LoginView /> : null}

      <ToastHost />
    </div>
  )
}

const NAV_ITEMS: { tab: Tab; label: string; icon: typeof CalendarIcon }[] = [
  { tab: 'today', label: '今日', icon: CalendarIcon },
  { tab: 'stats', label: '统计', icon: ChartIcon },
  { tab: 'manage', label: '管理', icon: SlidersIcon },
  { tab: 'me', label: '我的', icon: UserIcon },
]

function navItems(isAdmin: boolean) {
  return NAV_ITEMS.filter((item) => item.tab !== 'manage' || isAdmin)
}

/**
 * The desktop rail: brand, vertical navigation and the session. Hidden by
 * CSS below 1024px, where the bottom navigation takes over — same DOM, one
 * source of truth for the items, the media query decides which shell shows.
 */
function Sidebar({ tab, isAdmin }: { tab: Tab; isAdmin: boolean }) {
  const { session } = useApp()
  return (
    <aside className="sidebar">
      <button type="button" className="sidebar-brand" onClick={() => navigate('#today')} aria-label="水花打卡首页">
        <SealMark size={30} className="brand-seal" />
        <span className="sidebar-brand-text">
          <span className="wordmark">水花打卡</span>
          <span className="sidebar-tagline">把每一天盖成印章</span>
        </span>
      </button>
      <nav className="sidebar-nav" aria-label="主导航">
        {navItems(isAdmin).map((item) => {
          const Icon = item.icon
          const active = tab === item.tab
          return (
            <button
              key={item.tab}
              type="button"
              className="sidebar-item"
              data-active={active ? 'true' : undefined}
              aria-current={active ? 'page' : undefined}
              onClick={() => navigate(`#${item.tab}`)}
            >
              <Icon size={19} strokeWidth={active ? 2.1 : 1.8} />
              <span>{item.label}</span>
            </button>
          )
        })}
      </nav>
      <div className="sidebar-foot">
        {isAdmin ? (
          <span className="topbar-role" data-admin="true">
            管理员 · {session.username ?? 'admin'}
          </span>
        ) : (
          <button type="button" className="topbar-login" onClick={() => navigate('#login')}>
            <LockIcon size={13} /> 管理员登录
          </button>
        )}
      </div>
    </aside>
  )
}

function TopBar() {
  const { isAdmin } = useApp()
  return (
    <header className="topbar">
      <button type="button" className="brand" onClick={() => navigate('#today')} aria-label="水花打卡首页">
        <SealMark size={24} className="brand-seal" />
        <span className="wordmark">水花打卡</span>
      </button>
      {isAdmin ? (
        <span className="topbar-role" data-admin="true">
          管理员
        </span>
      ) : (
        <button type="button" className="topbar-login" onClick={() => navigate('#login')}>
          <LockIcon size={13} /> 登录
        </button>
      )}
    </header>
  )
}

function BottomNav({ tab, isAdmin }: { tab: Tab; isAdmin: boolean }) {
  return (
    <nav className="bottom-nav" aria-label="主导航">
      {navItems(isAdmin).map((item) => {
        const Icon = item.icon
        const active = tab === item.tab
        return (
          <button
            key={item.tab}
            type="button"
            className="nav-item"
            data-active={active ? 'true' : undefined}
            aria-current={active ? 'page' : undefined}
            onClick={() => navigate(`#${item.tab}`)}
          >
            <Icon size={21} strokeWidth={active ? 2.1 : 1.8} />
            <span>{item.label}</span>
          </button>
        )
      })}
    </nav>
  )
}

function BootFailure({ error, onRetry }: { error: unknown; onRetry: () => void }) {
  return (
    <div className="boot">
      <div className="boot-bar">
        <SealMark size={24} className="boot-seal" />
        <span className="wordmark">水花打卡</span>
      </div>
      <div className="boot-panel">
        <h2 className="panel-heading">页面无法加载</h2>
        <p className="boot-message">{describeError(error)}</p>
        <p className="boot-hint">
          前端通过同源 <code>/api/v1</code> 访问后端。开发模式下 Vite 会把请求代理到{' '}
          <code>127.0.0.1:8080</code>，请确认 Go 服务正在监听。
        </p>
        <Button onClick={onRetry}>重试</Button>
      </div>
    </div>
  )
}
