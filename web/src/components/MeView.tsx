import { useState, type ReactNode } from 'react'
import { api } from '../api/client'
import type { Health, VersionInfo } from '../api/types'
import type { ThemeChoice } from '../lib/theme'
import { useApp } from '../hooks/useApp'
import { navigate } from '../hooks/useNow'
import { describeError } from '../lib/errors'
import { DownloadIcon, LogInIcon, LogOutIcon, MonitorIcon, MoonIcon, SealMark, SunIcon, UserIcon } from './icons'
import { ServicePanel } from './ServicePanel'
import { Button, Segmented } from './ui'

const THEME_OPTIONS: { value: ThemeChoice; label: ReactNode }[] = [
  { value: 'light', label: <span className="theme-option"><SunIcon size={15} /> 浅色</span> },
  { value: 'dark', label: <span className="theme-option"><MoonIcon size={15} /> 深色</span> },
  { value: 'system', label: <span className="theme-option"><MonitorIcon size={15} /> 系统</span> },
]

/**
 * The "me" page: session, appearance, diagnostics and backup. Anonymous
 * visitors get the login door; the admin gets everything else.
 */
export function MeView() {
  const { session, isAdmin, projects, theme, setTheme, refreshSession, toast } = useApp()
  const [diagnostics, setDiagnostics] = useState<{ health: Health; version: VersionInfo } | null>(null)
  const [diagOpen, setDiagOpen] = useState(false)
  const [busy, setBusy] = useState(false)

  const logout = async () => {
    setBusy(true)
    try {
      await api.logout()
      await refreshSession()
      toast('已退出登录', 'info')
    } catch (error) {
      toast(describeError(error), 'error')
    } finally {
      setBusy(false)
    }
  }

  const openDiagnostics = async (open: boolean) => {
    setDiagOpen(open)
    if (open && diagnostics === null) {
      try {
        const [health, version] = await Promise.all([api.health(), api.version()])
        setDiagnostics({ health, version })
      } catch (error) {
        toast(describeError(error), 'error')
      }
    }
  }

  return (
    <div className="view me">
      <h1 className="view-title">我的</h1>

      <section className="me-card" aria-label="账户">
        <span className="me-avatar" data-admin={isAdmin ? 'true' : undefined}>
          {isAdmin ? <SealMark size={30} /> : <UserIcon size={26} />}
        </span>
        <div className="me-id">
          <strong>{isAdmin ? (session.username ?? 'admin') : '匿名访客'}</strong>
          <span>{isAdmin ? '管理员 · 可打卡与管理' : '仅浏览公开项目'}</span>
        </div>
        {isAdmin ? (
          <Button variant="ghost" pending={busy} onClick={() => void logout()}>
            <LogOutIcon size={15} /> 退出
          </Button>
        ) : (
          <Button onClick={() => navigate('#login')}>
            <LogInIcon size={15} /> 登录
          </Button>
        )}
      </section>

      <section className="detail-section panel chart-panel" aria-label="外观">
        <h2 className="section-head">外观</h2>
        <Segmented<ThemeChoice>
          ariaLabel="主题"
          value={theme}
          onChange={setTheme}
          options={THEME_OPTIONS}
        />
      </section>

      {isAdmin ? (
        <section className="detail-section panel chart-panel" aria-label="数据">
          <h2 className="section-head">数据</h2>
          <p className="sheet-note">导出全部分类、项目、打卡与跳过记录（JSON）。数据保存在服务器 SQLite 中。</p>
          <a className="button" data-variant="soft" href={api.exportUrl} download="splash-export.json">
            <DownloadIcon size={15} /> 导出备份
          </a>
        </section>
      ) : null}

      <section className="detail-section panel chart-panel" aria-label="诊断">
        <details
          className="diagnostics"
          open={diagOpen}
          onToggle={(event) => void openDiagnostics((event.currentTarget as HTMLDetailsElement).open)}
        >
          <summary className="section-head">服务诊断</summary>
          {diagnostics === null ? (
            <p className="sheet-note">展开后读取服务状态…</p>
          ) : (
            <ServicePanel health={diagnostics.health} version={diagnostics.version} projectCount={projects.length} />
          )}
        </details>
      </section>

      <p className="me-about">
        水花打卡 — Go + React 单二进制，SQLite 存储，数据只留在本机。
        <br />
        把每一天盖成印章。
      </p>
    </div>
  )
}
