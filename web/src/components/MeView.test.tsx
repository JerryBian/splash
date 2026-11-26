import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { AppProvider } from '../hooks/useApp'
import { callTo, stubFetch } from '../test/http'
import { makeBootstrap } from '../test/fixtures'
import { MeView } from './MeView'

describe('MeView database reset', () => {
  it('requires the admin password before enabling the destructive action', async () => {
    const fetchMock = stubFetch({
      '/api/v1/admin/reset': {
        method: 'POST',
        status: 422,
        body: {
          error: {
            code: 'validation_failed',
            message: 'the administrator password is required to reset the database',
            details: [{ field: 'password', message: 'is incorrect' }],
          },
        },
      },
    })
    render(
      <AppProvider initial={makeBootstrap()}>
        <MeView />
      </AppProvider>,
    )

    await userEvent.click(screen.getByRole('button', { name: '重置数据库' }))
    const confirm = screen.getByRole('button', { name: '删除并重置' })
    expect(confirm).toBeDisabled()

    await userEvent.type(screen.getByLabelText('管理员密码'), 'wrong-password')
    expect(confirm).toBeEnabled()
    await userEvent.click(confirm)

    expect(await screen.findByText('不正确')).toBeInTheDocument()
    const post = callTo(fetchMock, '/admin/reset', 'POST')
    expect(post?.body).toEqual({ password: 'wrong-password' })
    // A rejected confirmation leaves the user on the page, not in a reload.
    await waitFor(() => expect(screen.getByRole('alertdialog', { name: '重置数据库？' })).toBeInTheDocument())
  })
})
