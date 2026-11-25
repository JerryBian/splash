import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { callTo, stubFetch } from '../test/http'
import { LoginGate } from './LoginView'

describe('LoginGate', () => {
  it('shows a uniform error on bad credentials', async () => {
    stubFetch({
      '/api/v1/auth/login': {
        status: 401,
        body: { error: { code: 'unauthorized', message: 'username or password is incorrect' } },
      },
    })
    const onSuccess = vi.fn()
    render(<LoginGate onSuccess={onSuccess} />)
    await userEvent.type(screen.getByLabelText('用户名'), 'admin')
    await userEvent.type(screen.getByLabelText('密码'), 'wrong')
    await userEvent.click(screen.getByText('登录'))
    expect(await screen.findByRole('alert')).toBeInTheDocument()
    expect(onSuccess).not.toHaveBeenCalled()
  })

  it('signs in and hands control back to the app', async () => {
    const fetchMock = stubFetch({
      '/api/v1/auth/login': { role: 'admin', username: 'admin' },
    })
    const onSuccess = vi.fn()
    render(<LoginGate onSuccess={onSuccess} />)
    await userEvent.type(screen.getByLabelText('用户名'), 'admin')
    await userEvent.type(screen.getByLabelText('密码'), 'splash123')
    await userEvent.click(screen.getByText('登录'))

    await waitFor(() => {
      expect(onSuccess).toHaveBeenCalled()
    })
    const post = callTo(fetchMock, '/auth/login', 'POST')
    expect(post?.body).toEqual({ username: 'admin', password: 'splash123' })
  })

  it('validates empty input locally', async () => {
    const fetchMock = stubFetch({})
    render(<LoginGate onSuccess={vi.fn()} />)
    await userEvent.click(screen.getByText('登录'))
    expect(await screen.findByText('请输入用户名和密码')).toBeInTheDocument()
    expect(callTo(fetchMock, '/auth/login', 'POST')).toBeUndefined()
  })
})
