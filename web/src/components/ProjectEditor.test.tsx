import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { AppProvider } from '../hooks/useApp'
import { callTo, stubFetch } from '../test/http'
import { makeBootstrap, makeProject, TEST_TODAY } from '../test/fixtures'
import { ProjectEditor } from './ProjectEditor'

const adminBootstrap = makeBootstrap({ session: { role: 'admin', username: 'admin' } })

function routes() {
  return {
    '/api/v1/projects': [
      { method: 'POST', status: 201, body: makeProject({ name: '晨跑' }) },
      { body: { projects: [], today: TEST_TODAY } },
    ],
    '/api/v1/categories': { categories: [] },
    '/api/v1/stats/overview': makeBootstrap().overview,
  }
}

describe('ProjectEditor', () => {
  it('rejects an empty name without calling the server', async () => {
    const fetchMock = stubFetch(routes())
    render(
      <AppProvider initial={adminBootstrap}>
        <ProjectEditor projectId="new" />
      </AppProvider>,
    )
    await userEvent.click(screen.getByText('创建项目'))
    expect(await screen.findByText('请填写项目名称')).toBeInTheDocument()
    expect(callTo(fetchMock, '/projects', 'POST')).toBeUndefined()
  })

  it('creates a project with the chosen schedule payload', async () => {
    const fetchMock = stubFetch(routes())
    render(
      <AppProvider initial={adminBootstrap}>
        <ProjectEditor projectId="new" />
      </AppProvider>,
    )

    await userEvent.type(screen.getByLabelText('名称'), '晨跑')
    await userEvent.click(screen.getByText('每周'))
    await userEvent.click(screen.getByText('累计数量'))
    await userEvent.clear(screen.getByLabelText('每周目标'))
    await userEvent.type(screen.getByLabelText('每周目标'), '3')
    await userEvent.click(screen.getByText('创建项目'))

    await waitFor(() => {
      const post = callTo(fetchMock, '/projects', 'POST')
      expect(post).toBeDefined()
      expect(post?.body).toMatchObject({ name: '晨跑', schedule: 'weekly', mode: 'value', target: 3 })
    })
  })

  it('requires at least one weekday for the weekdays schedule', async () => {
    stubFetch(routes())
    render(
      <AppProvider initial={adminBootstrap}>
        <ProjectEditor projectId="new" />
      </AppProvider>,
    )
    await userEvent.type(screen.getByLabelText('名称'), '力量')
    await userEvent.click(screen.getByText('每周几'))
    // Default selection is Mon/Wed/Fri; clear all three.
    await userEvent.click(screen.getByText('一'))
    await userEvent.click(screen.getByText('三'))
    await userEvent.click(screen.getByText('五'))
    await userEvent.click(screen.getByText('创建项目'))
    expect(await screen.findByText('至少选择一天')).toBeInTheDocument()
  })

  it('loads an existing project into the form', () => {
    stubFetch(routes())
    const existing = makeProject({ id: 'p-9', name: '阅读', mode: 'value', target: 30, unit: '分钟', schedule: 'daily' })
    render(
      <AppProvider initial={makeBootstrap({ session: { role: 'admin', username: 'admin' }, projects: [existing] })}>
        <ProjectEditor projectId="p-9" />
      </AppProvider>,
    )
    expect(screen.getByLabelText('名称')).toHaveValue('阅读')
    expect(screen.getByLabelText('每天目标')).toHaveValue(30)
    expect(screen.getByText('保存修改')).toBeInTheDocument()
  })
})
