import { render, screen, waitFor, within } from '@testing-library/react'
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
    const water = makeProject({ id: 'p-2', name: '喝水', icon: '💧', mode: 'value', target: 8, unit: '杯' })
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

  it('asks for confirmation before checking while a prerequisite is unmet', async () => {
    const gated = makeProject({
      id: 'p-3',
      name: '刷短视频',
      status: makeStatus({ unmet: ['p-1'] }),
      prerequisites: ['p-1'],
    })
    const fetchMock = stubFetch({
      '/api/v1/projects/p-3/checkins': {
        method: 'POST',
        status: 201,
        body: { id: 'c9', projectId: 'p-3', value: 1, localDate: TEST_TODAY, checkedAt: '', tzOffset: 0, backfilled: false, violated: true, unmet: ['p-1'], createdAt: '' },
      },
      ...refreshRoutes([gated]),
    })
    render(
      <AppProvider initial={makeBootstrap({ session: { role: 'admin', username: 'admin' }, projects: [gated] })}>
        <TodayView />
      </AppProvider>,
    )

    // The card surfaces the closed gate.
    expect(screen.getByText(/需先完成 1 个前置/)).toBeInTheDocument()

    await userEvent.click(screen.getByLabelText('为 刷短视频 打卡'))
    // No POST yet: a confirmation dialog must appear first.
    expect(callTo(fetchMock, '/checkins', 'POST')).toBeUndefined()
    expect(await screen.findByText('前置条件未满足')).toBeInTheDocument()

    await userEvent.click(screen.getByText('仍要打卡'))
    await waitFor(() => {
      expect(callTo(fetchMock, '/checkins', 'POST')).toBeDefined()
    })
  })

  it('opens the record sheet for a bad habit instead of quick-checking', async () => {
    const bad = makeProject({
      id: 'p-bad',
      name: '熬夜',
      icon: '🦉',
      nature: 'bad',
      status: makeStatus({ status: 'done' }),
    })
    const fetchMock = stubFetch({
      '/api/v1/projects/p-bad/checkins': { checkins: [] },
      '/api/v1/projects/p-bad/skips': { skips: [] },
      ...refreshRoutes([bad]),
    })
    render(
      <AppProvider initial={makeBootstrap({ projects: [bad] })}>
        <TodayView />
      </AppProvider>,
    )

    // A lapse is deliberate: the stamp opens the sheet, no POST fires yet.
    await userEvent.click(screen.getByLabelText('记录 熬夜 破戒'))
    expect(await screen.findByRole('dialog', { name: '熬夜' })).toBeInTheDocument()
    expect(screen.getByText('记录破戒')).toBeInTheDocument()
    expect(callTo(fetchMock, '/checkins', 'POST')).toBeUndefined()
  })

  it('floats pinned projects into their own group, exactly once', () => {
    const pinned = makeProject({ id: 'p-pin', name: '喝水', icon: '💧', pinned: true })
    const normal = makeProject({ id: 'p-normal', name: '冥想' })
    stubFetch(refreshRoutes([pinned, normal]))
    render(
      <AppProvider initial={makeBootstrap({ session: { role: 'admin', username: 'admin' }, projects: [pinned, normal] })}>
        <TodayView />
      </AppProvider>,
    )

    const pinnedGroup = screen.getByRole('region', { name: '置顶' })
    expect(within(pinnedGroup).getByText('喝水')).toBeInTheDocument()
    expect(within(pinnedGroup).queryByText('冥想')).toBeNull()
    // The pinned card left its category group rather than being copied there.
    expect(screen.getAllByText('喝水')).toHaveLength(1)
    expect(screen.getByText('冥想')).toBeInTheDocument()
  })

  it('shows the postrequisite commitment state on a completed card', () => {
    const pending = makeProject({
      id: 'p-pending', name: '跑步', icon: '🏃',
      status: makeStatus({ status: 'done', value: 1, todayValue: 1, pendingPosts: ['p-2'] }),
    })
    const broken = makeProject({
      id: 'p-broken', name: '晨跑', icon: '🌅',
      status: makeStatus({ status: 'done', value: 1, todayValue: 1, brokenPosts: ['p-2'] }),
    })
    stubFetch(refreshRoutes([pending, broken]))
    render(
      <AppProvider initial={makeBootstrap({ session: { role: 'admin', username: 'admin' }, projects: [pending, broken] })}>
        <TodayView />
      </AppProvider>,
    )
    expect(screen.getByText('待兑现 1 个后置')).toBeInTheDocument()
    expect(screen.getByText('未兑现 1 个后置')).toBeInTheDocument()
  })

  it('invites the admin to create the first project when empty', () => {
    stubFetch(refreshRoutes([]))
    render(
      <AppProvider initial={makeBootstrap({ session: { role: 'admin', username: 'admin' }, projects: [], overview: makeOverview({ today: { date: TEST_TODAY, total: 0, done: 0, pending: 0, skipped: 0, due: 0, slipped: 0 }, totals: { checkins: 0, activeProjects: 0, bestStreak: 0, monthCheckins: 0, slips: 0, monthSlips: 0, postReqs: 0, postViolations: 0 } }) })}>
        <TodayView />
      </AppProvider>,
    )
    expect(screen.getByText('今天还没有打卡项目')).toBeInTheDocument()
    expect(screen.getAllByText('新建项目').length).toBeGreaterThan(0)
  })
})
