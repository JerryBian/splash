import { useState } from 'react'
import { api } from '../api/client'
import { useApp } from '../hooks/useApp'
import { describeError } from '../lib/errors'
import { SealMark } from './icons'
import { Sheet } from './overlays'
import { Button, Field, Input } from './ui'

function closeLogin(): void {
  window.history.back()
}

/**
 * The admin door. One account, credentials from the server's environment;
 * failures answer uniformly and the server rate-limits guesses. On success
 * the whole app re-bootstraps its data because visibility just changed.
 */
export function LoginView() {
  const { refreshSession, toast } = useApp()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    if (username.trim() === '' || password === '') {
      setError('请输入用户名和密码')
      return
    }
    setBusy(true)
    setError('')
    try {
      const session = await api.login(username.trim(), password)
      await refreshSession()
      toast(`欢迎回来，${session.username ?? '管理员'}`, 'ok')
      closeLogin()
    } catch (caught) {
      setError(describeError(caught))
      setBusy(false)
    }
  }

  return (
    <Sheet open onClose={closeLogin} title="管理员登录">
      <div className="login-hero" aria-hidden="true">
        <SealMark size={44} />
      </div>
      <p className="sheet-note login-note">
        登录后可打卡、管理项目，并查看仅登录可见的项目。账户由服务器环境变量配置。
      </p>
      <form
        className="login-form"
        onSubmit={(event) => {
          event.preventDefault()
          void submit()
        }}
      >
        <Field label="用户名" htmlFor="login-username">
          <Input
            id="login-username"
            value={username}
            autoComplete="username"
            onChange={(event) => setUsername(event.target.value)}
          />
        </Field>
        <Field label="密码" htmlFor="login-password">
          <Input
            id="login-password"
            type="password"
            value={password}
            autoComplete="current-password"
            onChange={(event) => setPassword(event.target.value)}
          />
        </Field>
        {error !== '' ? (
          <p className="form-error" role="alert" data-shake="true">
            {error}
          </p>
        ) : null}
        <Button type="submit" pending={busy} className="login-submit">
          登录
        </Button>
      </form>
    </Sheet>
  )
}
