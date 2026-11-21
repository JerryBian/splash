import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { AppProvider } from '../hooks/useApp'
import { callTo, stubFetch } from '../test/http'
import { makeBootstrap, makeOverview, makeProject, makeStatus, TEST_TODAY } from '../test/fixtures'
import { TodayView } from './TodayView'

const adminBootstrap = makeBootstrap({ session: { role: 'admin', username: 'admin' } })

function refreshRoutes(projects = [makeProject()]) {
  return {
    '/api/v1/projects': { projects, today: TEST_TODAY },
    '/api/v1/categories': { categories: [] },
    '/api/v1/stats/overview': makeOverview(),
  }
}

describe('TodayView', () => {
  it('shows the date header and project cards', () => {
    stubFetch(refreshRoutes())
    render(
      <AppProvider initial={adminBootstrap}>
        <TodayView />
      </AppProvider>,
    )
    expect(screen.getByText('11月')).toBeInTheDocument()
    expect(screen.getByText('冥想')).toBeInTheDocument()
    expect(screen.getByText('每天 1 次')).toBeInTheDocument()
  })

  it('quick-checks a one-tap project and stamps the seal', async () => {
    const fetchMock = stubFetch({
      '/api/v1/projects/p-1/checkins': {
        method: 'POST',
        status: 201,
        body: { id: 'c1', projectId: 'p-1', value: 1, localDate: TEST_TODAY, checkedAt: '', tzOffset: 0, createdAt: '' },
      },
      ...refreshRoutes([makeProject({ status: makeStatus({ status: 'done', value: 1, todayValue: 1, count: 1, streak: 1 }) })]),
    })
    render(
      <AppProvider initial={adminBootstrap}>
        <TodayView />
      </AppProvider>,
    )

    await userEvent.click(screen.getByLabelText('为 冥想 打卡'))

    // Optimistic: the seal appears before the server answers.
    await waitFor(() => {
      expect(screen.getByLabelText('冥想 已完成，查看记录')).toBeInTheDocument()
    })
    const post = callTo(fetchMock, '/checkins', 'POST')
    expect(post?.body).toMatchObject({ value: 1 })
    // Then the server truth arrives via refresh.
    await waitFor(() => {
      expect(callTo(fetchMock, '/projects')).toBeDefined()
    })
  })

  it('opens the record sheet for measured goals instead of quick-checking', async () => {
    const water = makeProject({ id: 'p-2', name: '喝水', icon: '💧', target: 8, unit: '杯' })
    stubFetch({
      '/api/v1/projects/p-2/checkins': { checkins: [] },
      '/api/v1/projects/p-2/skips': { skips: [] },
      ...refreshRoutes([water]),
    })
    render(
      <AppProvider initial={makeBootstrap({ session: { role: 'admin', username: 'admin' }, projects: [water] })}>
        <TodayView />
      </AppProvider>,
    )

    await userEvent.click(screen.getByLabelText('为 喝水 打卡'))
    expect(await screen.findByRole('dialog', { name: '喝水' })).toBeInTheDocument()
    expect(screen.getByLabelText('数量（杯）')).toBeInTheDocument()
  })

  it('shows the anonymous banner and no stamp buttons for visitors', () => {
    stubFetch(refreshRoutes())
    render(
      <AppProvider initial={makeBootstrap()}>
        <TodayView />
      </AppProvider>,
    )
    expect(screen.getByText(/访客模式/)).toBeInTheDocument()
    expect(screen.queryByLabelText('为 冥想 打卡')).not.toBeInTheDocument()
  })

  it('invites the admin to create the first project when empty', () => {
    stubFetch(refreshRoutes([]))
    render(
      <AppProvider initial={makeBootstrap({ session: { role: 'admin', username: 'admin' }, projects: [], overview: makeOverview({ today: { date: TEST_TODAY, total: 0, done: 0, pending: 0, skipped: 0, due: 0 }, totals: { checkins: 0, activeProjects: 0, bestStreak: 0, monthCheckins: 0 } }) })}>
        <TodayView />
      </AppProvider>,
    )
    expect(screen.getByText('今天还没有打卡项目')).toBeInTheDocument()
    expect(screen.getAllByText('新建项目').length).toBeGreaterThan(0)
  })
})
