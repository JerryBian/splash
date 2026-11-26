import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import { AppProvider } from '../hooks/useApp'
import { stubFetch } from '../test/http'
import { invalidate } from '../lib/async'
import { makeBootstrap, makeProject, makeStatus, TEST_TODAY } from '../test/fixtures'
import { ProjectDetail } from './ProjectDetail'
import type { ProjectStats } from '../api/types'

// The useLoaded cache is module-level; clear it so each test fetches fresh.
beforeEach(() => invalidate(''))

function makeStats(project = makeProject()): ProjectStats {
  return {
    project,
    summary: makeStatus(),
    totals: {
      checkins: 0, value: 0, activeDays: 0, done: 0, missed: 0, skipped: 0,
      completionRate: 0, onTimeRate: 0, onTimeCount: 0, lateCount: 0, avgTime: '', backfilled: 0, violations: 0,
    },
    heatmap: [],
    periods: [],
    recent: [],
    skips: [],
  }
}

// A project that started years ago, so the calendar can step back past years.
const oldProject = makeProject({ startDate: '2024-01-01' })

describe('ProjectDetail history', () => {
  it('steps the calendar back to a past year and fetches that year', async () => {
    const fetchMock = stubFetch({
      '/api/v1/projects/p-1/stats': makeStats(oldProject),
      '/api/v1/projects/p-1/checkins': { checkins: [] },
      '/api/v1/projects/p-1/skips': { skips: [] },
    })
    await act(async () => {
      render(
        <AppProvider initial={makeBootstrap({ projects: [oldProject] })}>
          <ProjectDetail projectId="p-1" />
        </AppProvider>,
      )
    })

    expect(screen.getByText(`${TEST_TODAY.slice(0, 4)} 年 · 至今`)).toBeInTheDocument()
    await userEvent.click(screen.getByLabelText('上一年'))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find((c) => String(c[0]).includes('date=2025-12-31'))
      expect(call).toBeDefined()
    })
    expect(screen.getByText('2025 年')).toBeInTheDocument()
  })

  it('shows punctuality, the average time and a late badge for a time habit', async () => {
    const timeProject = makeProject({ id: 'p-t', name: '准时睡觉', mode: 'time', targetTime: '23:00' })
    const stats = makeStats(timeProject)
    stats.summary = makeStatus({ status: 'done', value: 1, target: 1 })
    stats.totals = { ...stats.totals, checkins: 2, done: 2, onTimeRate: 0.5, onTimeCount: 1, lateCount: 1, avgTime: '23:05' }
    stats.recent = [
      {
        id: 'c-late', projectId: 'p-t', value: 1, time: '23:30', hasPhoto: true,
        localDate: '2026-11-20', checkedAt: '2026-11-20T15:30:00Z', tzOffset: 480,
        backfilled: false, violated: false, unmet: [], createdAt: '',
      },
      {
        id: 'c-ok', projectId: 'p-t', value: 1, time: '22:40', hasPhoto: false,
        localDate: '2026-11-19', checkedAt: '2026-11-19T14:40:00Z', tzOffset: 480,
        backfilled: false, violated: false, unmet: [], createdAt: '',
      },
    ]
    stubFetch({
      '/api/v1/projects/p-t/stats': stats,
      '/api/v1/projects/p-t/checkins': { checkins: [] },
      '/api/v1/projects/p-t/skips': { skips: [] },
    })
    await act(async () => {
      render(
        <AppProvider initial={makeBootstrap({ projects: [timeProject] })}>
          <ProjectDetail projectId="p-t" />
        </AppProvider>,
      )
    })

    expect(screen.getByText('准时率')).toBeInTheDocument()
    expect(screen.getByText('50%')).toBeInTheDocument()
    expect(screen.getByText('平均时间')).toBeInTheDocument()
    expect(screen.getByText('23:05')).toBeInTheDocument()
    // The history shows the recorded clock, not the checked_at instant.
    expect(screen.getByText('23:30')).toBeInTheDocument()
    expect(screen.getByText('迟到')).toBeInTheDocument()
    expect(screen.getByAltText('打卡照片')).toBeInTheDocument()
  })

  it('loads older records on demand', async () => {    const recent = Array.from({ length: 30 }, (_, i) => ({
      id: `c${i}`,
      projectId: 'p-1',
      value: 1,
      localDate: `2026-11-${String(21 - Math.floor(i / 2)).padStart(2, '0')}`,
      checkedAt: '',
      tzOffset: 0,
      backfilled: false,
      violated: false,
      hasPhoto: false,
      unmet: [],
      createdAt: '',
    }))
    const stats = makeStats(oldProject)
    stats.recent = recent
    const fetchMock = stubFetch({
      '/api/v1/projects/p-1/stats': stats,
      '/api/v1/projects/p-1/checkins': { checkins: [] },
      '/api/v1/projects/p-1/skips': { skips: [] },
    })
    await act(async () => {
      render(
        <AppProvider initial={makeBootstrap({ projects: [oldProject] })}>
          <ProjectDetail projectId="p-1" />
        </AppProvider>,
      )
    })

    // 30 recent records means there may be older ones: the load-more button shows.
    await userEvent.click(screen.getByText('加载更早记录'))
    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        (c) => String(c[0]).includes('/checkins') && String(c[0]).includes('to='),
      )
      expect(call).toBeDefined()
    })
  })
})
