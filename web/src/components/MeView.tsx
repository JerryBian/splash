import { useRef, useState, type ChangeEvent, type ReactNode } from 'react'
import { api } from '../api/client'
import type { ExportData, Health, VersionInfo } from '../api/types'
import type { ThemeChoice } from '../lib/theme'
import { useApp } from '../hooks/useApp'
import { describeError, fieldErrorsFrom } from '../lib/errors'
import { DownloadIcon, LogOutIcon, MonitorIcon, MoonIcon, SealMark, SunIcon, TrashIcon, UploadIcon } from './icons'
import { ConfirmDialog } from './overlays'
import { ServicePanel } from './ServicePanel'
import { Button, Field, Input, Segmented } from './ui'

const THEME_OPTIONS: { value: ThemeChoice; label: ReactNode }[] = [
  { value: 'light', label: <span className="theme-option"><SunIcon size={15} /> 浅色</span> },
  { value: 'dark', label: <span className="theme-option"><MoonIcon size={15} /> 深色</span> },
  { value: 'system', label: <span className="theme-option"><MonitorIcon size={15} /> 系统</span> },
]

/**
 * The "me" page: account, appearance, diagnostics and backup. The whole app
 * sits behind the administrator login, so this is always the admin's own
 * account — signing out returns to the login gate.
 */
export function MeView() {
  const { session, projects, theme, setTheme, logout, refresh, toast } = useApp()
  const [diagnostics, setDiagnostics] = useState<{ health: Health; version: VersionInfo } | null>(null)
  const [diagOpen, setDiagOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [importing, setImporting] = useState(false)
  const [resetOpen, setResetOpen] = useState(false)
  const [resetPassword, setResetPassword] = useState('')
  const [resetError, setResetError] = useState('')
  const [resetting, setResetting] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)

  const onImportFile = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    // Reset so picking the same file again still fires onChange.
    event.target.value = ''
    if (!file) return
    setImporting(true)
    try {
      const text = await file.text()
      let data: ExportData
      try {
        data = JSON.parse(text) as ExportData
      } catch {
        toast('这个文件不是有效的 JSON 备份', 'error')
        return
      }
      const counts = await api.importData(data)
      toast(
        `导入完成：项目 ${counts.projects} · 打卡 ${counts.checkins} · 分类 ${counts.categories} · 跳过 ${counts.skips}`,
        'ok',
      )
      await refresh()
    } catch (error) {
      toast(describeError(error), 'error')
    } finally {
      setImporting(false)
    }
  }

  const signOut = async () => {
    setBusy(true)
    try {
      await logout()
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

  const openReset = () => {
    setResetPassword('')
    setResetError('')
    setResetOpen(true)
  }

  const closeReset = () => {
    if (resetting) return
    setResetOpen(false)
    setResetPassword('')
    setResetError('')
  }

  const doReset = async () => {
    if (resetting) return
    if (resetPassword === '') {
      setResetError('请输入管理员密码')
      return
    }
    setResetting(true)
    setResetError('')
    try {
      const result = await api.resetDatabase(resetPassword)
      setResetOpen(false)
      setResetting(false)
      toast(
        result.seededProjects > 0
          ? `数据库已重置，并恢复 ${result.seededProjects} 个示例项目`
          : '数据库已重置，回到初始状态',
        'ok',
      )
      // Every page caches its own loads and the provider holds the old lists;
      // a full reload is the only way to guarantee they all start from the
      // fresh database. The short delay lets the toast paint first.
      window.setTimeout(() => window.location.assign('/'), 450)
    } catch (error) {
      const fields = fieldErrorsFrom(error)
      setResetError(fields['password'] ?? describeError(error))
      setResetting(false)
    }
  }

  return (
    <div className="view me">
      <h1 className="view-title">我的</h1>

      <section className="me-card" aria-label="账户">
        <span className="me-avatar" data-admin="true">
          <SealMark size={30} />
        </span>
        <div className="me-id">
          <strong>{session.username ?? 'admin'}</strong>
          <span>管理员 · 可打卡与管理</span>
        </div>
        <Button variant="ghost" pending={busy} onClick={() => void signOut()}>
          <LogOutIcon size={15} /> 退出
        </Button>
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

      <section className="detail-section panel chart-panel" aria-label="数据">
        <h2 className="section-head">数据</h2>
        <p className="sheet-note">
          导出全部分类、项目、打卡与跳过记录（JSON）。导入按 ID 合并，不会覆盖现有数据，重复导入同一份备份不会新增。数据保存在服务器 SQLite 中。
        </p>
        <div className="data-actions">
          <a className="button" data-variant="soft" href={api.exportUrl} download="splash-export.json">
            <DownloadIcon size={15} /> 导出备份
          </a>
          <Button variant="soft" pending={importing} onClick={() => fileRef.current?.click()}>
            <UploadIcon size={15} /> 导入备份
          </Button>
          <input
            ref={fileRef}
            type="file"
            accept="application/json,.json"
            hidden
            onChange={(event) => void onImportFile(event)}
          />
        </div>
      </section>

      <section className="detail-section panel chart-panel" aria-label="危险操作">
        <h2 className="section-head">危险操作</h2>
        <p className="sheet-note">
          重置会删除服务器上的 SQLite 数据库文件并重新初始化；所有项目、打卡、分类、关联与跳过记录都会消失，无法撤销。请先导出备份。
        </p>
        <Button variant="danger" onClick={openReset}>
          <TrashIcon size={15} /> 重置数据库
        </Button>
      </section>

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

      <ConfirmDialog
        open={resetOpen}
        title="重置数据库？"
        message="这会删除数据库文件并回到初始状态，所有数据无法恢复。请输入管理员密码确认。"
        confirmLabel="删除并重置"
        danger
        pending={resetting}
        confirmDisabled={resetPassword === ''}
        onConfirm={() => void doReset()}
        onClose={closeReset}
      >
        <Field label="管理员密码" htmlFor="reset-password" error={resetError}>
          <Input
            id="reset-password"
            type="password"
            autoComplete="current-password"
            value={resetPassword}
            onChange={(event) => {
              setResetPassword(event.target.value)
              setResetError('')
            }}
            onKeyDown={(event) => {
              if (event.key === 'Enter' && resetPassword !== '') void doReset()
            }}
          />
        </Field>
      </ConfirmDialog>
    </div>
  )
}
