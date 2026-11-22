import { render, screen, waitFor, within } from '@testing-library/react'
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
    // The start date defaults to the server's day, not the device clock;
    // otherwise a device one day ahead makes the date input invalid and the
    // form's native submit is silently blocked.
    expect(screen.getByLabelText('开始日期')).toHaveValue(TEST_TODAY)
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

  it('starts a new project with an unused random ink', () => {
    stubFetch(routes())
    const { container } = render(
      <AppProvider initial={adminBootstrap}>
        <ProjectEditor projectId="new" />
      </AppProvider>,
    )
    const trigger = container.querySelector('.appearance-trigger')
    expect(trigger?.getAttribute('data-ink')).not.toBe('teal')
  })

  it('opens a compact appearance picker with colors and grouped icons', async () => {
    stubFetch(routes())
    render(
      <AppProvider initial={adminBootstrap}>
        <ProjectEditor projectId="new" />
      </AppProvider>,
    )

    await userEvent.click(screen.getByRole('button', { name: /图标与颜色/ }))
    const dialog = await screen.findByRole('dialog', { name: '图标与颜色' })
    expect(dialog.querySelectorAll('.color-swatch')).toHaveLength(16)
    expect(dialog.querySelectorAll('.emoji-option').length).toBeGreaterThan(20)

    const groups = within(dialog).getByRole('group', { name: '图标分类' })
    await userEvent.click(within(groups).getByRole('button', { name: /学习/ }))
    expect(dialog.querySelectorAll('.emoji-option')).toHaveLength(24)
    expect(within(dialog).getByRole('button', { name: '图标 🧠' })).toBeInTheDocument()
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
