import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { AppProvider } from '../hooks/useApp'
import { callTo, stubFetch } from '../test/http'
import { makeBootstrap, makeProject, TEST_TODAY } from '../test/fixtures'
import { CheckinSheet } from './CheckinSheet'

const timeProject = makeProject({ id: 'p-t', name: '准时睡觉', mode: 'time', targetTime: '23:00' })

describe('CheckinSheet time mode', () => {
  it('shows a time input and submits the recorded clock time', async () => {
    const fetchMock = stubFetch({
      '/api/v1/projects/p-t/checkins': [
        {
          method: 'POST',
          status: 201,
          body: {
            id: 'c1', projectId: 'p-t', value: 1, time: '22:40', localDate: TEST_TODAY,
            checkedAt: '', tzOffset: 0, backfilled: false, violated: false, unmet: [], createdAt: '',
          },
        },
        { checkins: [] },
      ],
      '/api/v1/projects/p-t/skips': { skips: [] },
      '/api/v1/projects': { projects: [timeProject], today: TEST_TODAY },
      '/api/v1/categories': { categories: [] },
      '/api/v1/stats/overview': makeBootstrap().overview,
    })
    await act(async () => {
      render(
        <AppProvider initial={makeBootstrap({ projects: [timeProject] })}>
          <CheckinSheet project={timeProject} initialDate={TEST_TODAY} onClose={() => {}} />
        </AppProvider>,
      )
    })

    // A time input (not a quantity stepper) is offered, with the deadline hint.
    expect(screen.getByLabelText('时间')).toBeInTheDocument()
    expect(screen.getByText(/准时目标 23:00/)).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /记录 \d{2}:\d{2}/ }))
    await waitFor(() => {
      const post = callTo(fetchMock, '/checkins', 'POST')
      expect(post).toBeDefined()
      expect(post?.body).toMatchObject({ value: 1 })
      const body = post?.body as { time?: string } | undefined
      expect(body?.time).toMatch(/^\d{2}:\d{2}$/)
    })
  })
})

describe('CheckinSheet proof photo', () => {
  it('reads a photo into a data URL and submits it with the record', async () => {
    const checkProject = makeProject({ id: 'p-c', name: '冥想', mode: 'check' })
    const fetchMock = stubFetch({
      '/api/v1/projects/p-c/checkins': [
        {
          method: 'POST',
          status: 201,
          body: {
            id: 'c2', projectId: 'p-c', value: 1, hasPhoto: true, localDate: TEST_TODAY,
            checkedAt: '', tzOffset: 0, backfilled: false, violated: false, unmet: [], createdAt: '',
          },
        },
        { checkins: [] },
      ],
      '/api/v1/projects/p-c/skips': { skips: [] },
      '/api/v1/projects': { projects: [checkProject], today: TEST_TODAY },
      '/api/v1/categories': { categories: [] },
      '/api/v1/stats/overview': makeBootstrap().overview,
    })
    await act(async () => {
      render(
        <AppProvider initial={makeBootstrap({ projects: [checkProject] })}>
          <CheckinSheet project={checkProject} initialDate={TEST_TODAY} onClose={() => {}} />
        </AppProvider>,
      )
    })

    // A hidden file input is the label target; jsdom cannot decode a real
    // picture, but a tiny PNG travels the direct data-URL path.
    const input = document.getElementById('checkin-photo') as HTMLInputElement
    const file = new File([new Uint8Array([0x89, 0x50, 0x4e, 0x47])], 'proof.png', { type: 'image/png' })
    fireEvent.change(input, { target: { files: [file] } })
    expect(await screen.findByAltText('打卡照片预览')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: '打卡' }))
    await waitFor(() => {
      const post = callTo(fetchMock, '/checkins', 'POST')
      expect(post).toBeDefined()
      expect((post?.body as { photo?: string } | undefined)?.photo).toMatch(/^data:image\/png;base64,/)
    })
  })
})
