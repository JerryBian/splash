import { useState } from 'react'
import { api } from '../api/client'
import { describeError } from '../lib/errors'
import { SealMark } from './icons'
import { Button, Field, Input } from './ui'

/**
 * The login gate: the whole screen when nobody is signed in. Every page and
 * every data route requires the administrator, so this is the only thing an
 * anonymous visitor sees. One account, credentials from the server's
 * environment; failures answer uniformly and the server rate-limits guesses.
 * On success the app re-bootstraps into the signed-in shell.
 */
export function LoginGate({ onSuccess }: { onSuccess: () => void }) {
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
      await api.login(username.trim(), password)
      onSuccess()
    } catch (caught) {
      setError(describeError(caught))
      setBusy(false)
    }
  }

  return (
    <div className="boot login-gate">
      <div className="boot-bar">
        <SealMark size={24} className="boot-seal" />
        <span className="wordmark">水花打卡</span>
      </div>
      <div className="boot-panel">
        <div className="login-hero" aria-hidden="true">
          <SealMark size={44} />
        </div>
        <h2 className="panel-heading">管理员登录</h2>
        <p className="boot-message">登录后即可打卡、管理项目与查看统计。账户由服务器环境变量配置。</p>
        <form
          className="login-form"
          noValidate
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
      </div>
    </div>
  )
}
