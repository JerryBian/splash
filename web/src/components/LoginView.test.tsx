import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { AppProvider } from '../hooks/useApp'
import { callTo, stubFetch } from '../test/http'
import { makeBootstrap, makeOverview, TEST_TODAY } from '../test/fixtures'
import { LoginView } from './LoginView'
import { ToastHost } from './overlays'

function renderLogin() {
  return render(
    <AppProvider initial={makeBootstrap()}>
      <LoginView />
      <ToastHost />
    </AppProvider>,
  )
}

describe('LoginView', () => {
  it('shows a uniform error on bad credentials', async () => {
    stubFetch({
      '/api/v1/auth/login': {
        status: 401,
        body: { error: { code: 'unauthorized', message: 'username or password is incorrect' } },
      },
    })
    renderLogin()
    await userEvent.type(screen.getByLabelText('用户名'), 'admin')
    await userEvent.type(screen.getByLabelText('密码'), 'wrong')
    await userEvent.click(screen.getByText('登录'))
    expect(await screen.findByRole('alert')).toBeInTheDocument()
  })

  it('signs in, refreshes the session and greets the admin', async () => {
    const fetchMock = stubFetch({
      '/api/v1/auth/login': { role: 'admin', username: 'admin' },
      '/api/v1/auth/me': { role: 'admin', username: 'admin' },
      '/api/v1/projects': { projects: [], today: TEST_TODAY },
      '/api/v1/categories': { categories: [] },
      '/api/v1/stats/overview': makeOverview({ today: { date: TEST_TODAY, total: 0, done: 0, pending: 0, skipped: 0, due: 0 }, totals: { checkins: 0, activeProjects: 0, bestStreak: 0, monthCheckins: 0 } }),
    })
    renderLogin()
    await userEvent.type(screen.getByLabelText('用户名'), 'admin')
    await userEvent.type(screen.getByLabelText('密码'), 'splash123')
    await userEvent.click(screen.getByText('登录'))

    await waitFor(() => {
      expect(screen.getByText('欢迎回来，admin')).toBeInTheDocument()
    })
    const post = callTo(fetchMock, '/auth/login', 'POST')
    expect(post?.body).toEqual({ username: 'admin', password: 'splash123' })
    // Visibility changed, so the data reloaded.
    await waitFor(() => {
      expect(callTo(fetchMock, '/projects')).toBeDefined()
    })
  })

  it('validates empty input locally', async () => {
    const fetchMock = stubFetch({})
    renderLogin()
    await userEvent.click(screen.getByText('登录'))
    expect(await screen.findByText('请输入用户名和密码')).toBeInTheDocument()
    expect(callTo(fetchMock, '/auth/login', 'POST')).toBeUndefined()
  })
})
