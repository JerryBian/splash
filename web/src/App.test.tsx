import { act, fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { App } from './App'
import { stubFetch } from './test/http'
import { makeOverview, makeProject, TEST_TODAY } from './test/fixtures'

/**
 * React 19's use() suspends the shell on first paint; the bootstrap promise
 * must settle inside an awaited act scope or the retry never flushes.
 */
async function renderApp() {
  await act(async () => {
    render(<App />)
  })
}

function adminRoutes() {
  return {
    '/api/v1/auth/me': { role: 'admin', username: 'admin' },
    '/api/v1/projects': { projects: [makeProject()], today: TEST_TODAY },
    '/api/v1/categories': { categories: [] },
    '/api/v1/stats/overview': makeOverview(),
  }
}

describe('App', () => {
  it('explains a failed bootstrap and offers a retry', async () => {
    stubFetch({})
    await renderApp()
    expect(screen.getByText('页面无法加载')).toBeInTheDocument()
    expect(screen.getByText('重试')).toBeInTheDocument()
  })

  it('recovers on retry when the server comes back', async () => {
    const fetchMock = stubFetch({})
    await renderApp()
    expect(screen.getByText('页面无法加载')).toBeInTheDocument()

    fetchMock.mockImplementation(async (input: RequestInfo | URL) => {
      const path = String(input).replace(/^https?:\/\/[^/]+/, '')
      const routes = adminRoutes() as Record<string, unknown>
      for (const [prefix, body] of Object.entries(routes)) {
        if (path.startsWith(prefix)) {
          return {
            status: 200,
            ok: true,
            statusText: '200',
            headers: { get: () => null },
            text: () => Promise.resolve(JSON.stringify(body)),
          } as unknown as Response
        }
      }
      return {
        status: 404,
        ok: false,
        statusText: '404',
        headers: { get: () => null },
        text: () => Promise.resolve('{"error":{"code":"not_found","message":"no"}}'),
      } as unknown as Response
    })

    // The retry suspends the shell again; the click and the bootstrap's
    // resolution must both happen inside one awaited act scope.
    await act(async () => {
      fireEvent.click(screen.getByText('重试'))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(screen.getByText('冥想')).toBeInTheDocument()
  })

  it('shows the login gate when nobody is signed in', async () => {
    const fetchMock = stubFetch({ '/api/v1/auth/me': { role: 'anonymous' } })
    await renderApp()
    expect(screen.getByText('管理员登录')).toBeInTheDocument()
    expect(screen.getByLabelText('用户名')).toBeInTheDocument()
    // An anonymous bootstrap fetches no data and renders no projects.
    expect(screen.queryByText('冥想')).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('boots into the Today page with every tab after an admin bootstrap', async () => {
    stubFetch(adminRoutes())
    await renderApp()
    expect(screen.getByText('冥想')).toBeInTheDocument()
    // Both shells (bottom nav + desktop rail) are in the DOM; CSS picks one.
    expect(screen.getAllByText('今日').length).toBeGreaterThan(0)
    expect(screen.getAllByText('统计').length).toBeGreaterThan(0)
    expect(screen.getAllByText('我的').length).toBeGreaterThan(0)
    // The whole app is the administrator's now: manage is always present.
    expect(screen.getAllByText('管理').length).toBeGreaterThan(0)
    expect(screen.getAllByText(/管理员/).length).toBeGreaterThan(0)
  })
})
