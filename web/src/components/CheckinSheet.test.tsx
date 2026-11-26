import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { AppProvider } from '../hooks/useApp'
import { callTo, stubFetch } from '../test/http'
import { makeBootstrap, makeProject, TEST_TODAY } from '../test/fixtures'
import { CheckinSheet } from './CheckinSheet'

const checkProject = makeProject({ id: 'p-c', name: '准时睡觉', mode: 'check', notePrompt: '什么时候睡的？睡了多久？' })

function routes() {
  return {
    '/api/v1/projects/p-c/checkins': [
      {
        method: 'POST',
        status: 201,
        body: {
          id: 'c1', projectId: 'p-c', value: 1, hasPhoto: false, localDate: TEST_TODAY,
          checkedAt: '', tzOffset: 0, backfilled: false, violated: false, unmet: [],
          postUnmet: [], postPending: [], createdAt: '',
        },
      },
      { checkins: [] },
    ],
    '/api/v1/projects/p-c/skips': { skips: [] },
    '/api/v1/projects': { projects: [checkProject], today: TEST_TODAY },
    '/api/v1/categories': { categories: [] },
    '/api/v1/stats/overview': makeBootstrap().overview,
  }
}

describe('CheckinSheet supplement', () => {
  it('uses the project prompt as the supplement heading and saves the text', async () => {
    const fetchMock = stubFetch(routes())
    await act(async () => {
      render(
        <AppProvider initial={makeBootstrap({ projects: [checkProject] })}>
          <CheckinSheet project={checkProject} initialDate={TEST_TODAY} onClose={() => {}} />
        </AppProvider>,
      )
    })

    expect(screen.getByText('什么时候睡的？睡了多久？')).toBeInTheDocument()
    await userEvent.type(screen.getByLabelText('什么时候睡的？睡了多久？'), '22:40 入睡，睡了 7 小时')
    await userEvent.click(screen.getByRole('button', { name: '打卡' }))
    await waitFor(() => {
      const post = callTo(fetchMock, '/checkins', 'POST')
      expect(post).toBeDefined()
      expect(post?.body).toMatchObject({ note: '22:40 入睡，睡了 7 小时', value: 1 })
    })
  })
})

describe('CheckinSheet proof photo', () => {
  it('reads a photo into a data URL and submits it with the record', async () => {
    const fetchMock = stubFetch(routes())
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
