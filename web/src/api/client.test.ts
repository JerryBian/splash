import { describe, expect, it, vi } from 'vitest'
import { callTo, stubFetch } from '../test/http'
import { makeOverview, makeProject, TEST_TODAY } from '../test/fixtures'
import { api, ApiError, loadBootstrap, NetworkError } from './client'
import { traceReset } from './trace'

describe('request plumbing', () => {
  it('flattens the error envelope into an ApiError', async () => {
    stubFetch({
      '/api/v1/projects': {
        status: 404,
        body: { error: { code: 'not_found', message: 'gone', requestId: 'r-1' } },
      },
    })
    const error = await api.listProjects().catch((e: unknown) => e)
    expect(error).toBeInstanceOf(ApiError)
    const apiError = error as ApiError
    expect(apiError.status).toBe(404)
    expect(apiError.code).toBe('not_found')
    expect(apiError.requestId).toBe('r-1')
  })

  it('keeps field details for forms', async () => {
    stubFetch({
      '/api/v1/projects': {
        status: 422,
        body: {
          error: {
            code: 'validation_failed',
            message: 'rejected',
            details: [{ field: 'name', message: 'is required' }],
          },
        },
      },
    })
    const error = (await api.createProject({ name: '', schedule: 'daily', startDate: TEST_TODAY }).catch(
      (e: unknown) => e,
    )) as ApiError
    expect(error.details).toHaveLength(1)
    expect(error.fieldSummary).toBe('name is required')
  })

  it('returns undefined for 204', async () => {
    stubFetch({ '/api/v1/checkins/x': { status: 204, body: null } })
    await expect(api.deleteCheckin('x')).resolves.toBeUndefined()
  })

  it('wraps network failures in NetworkError', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('failed to fetch')))
    const error = await api.health().catch((e: unknown) => e)
    expect(error).toBeInstanceOf(NetworkError)
  })

  it('turns non-JSON error pages into a readable ApiError', async () => {
    stubFetch({ '/api/v1/health': { status: 502, body: '<html>bad gateway</html>' } })
    const error = (await api.health().catch((e: unknown) => e)) as ApiError
    expect(error).toBeInstanceOf(ApiError)
    expect(error.code).toBe('unexpected_response')
    expect(error.message).toContain('502')
  })

  it('records the round trip for the diagnostics panel', async () => {
    traceReset()
    stubFetch({ '/api/v1/health': { status: 'ok', uptimeSeconds: 1, goroutines: 2, timestamp: '', version: 'dev' } })
    await api.health()
    const { readTrace } = await import('./trace')
    const trace = readTrace()
    expect(trace.state).toBe('settled')
    expect(trace.method).toBe('GET')
    expect(trace.path).toBe('/health')
    expect(trace.status).toBe(200)
  })
})

describe('api surface', () => {
  it('sends credentials as JSON on login', async () => {
    const fetchMock = stubFetch({
      '/api/v1/auth/login': { role: 'admin', username: 'admin' },
    })
    await api.login('admin', 'splash123')
    const call = callTo(fetchMock, '/auth/login', 'POST')
    expect(call?.body).toEqual({ username: 'admin', password: 'splash123' })
  })

  it('builds the projects query from params', async () => {
    const fetchMock = stubFetch({
      '/api/v1/projects': { projects: [], today: TEST_TODAY },
    })
    await api.listProjects({ date: TEST_TODAY, archived: true, category: 'c1', q: '水' })
    const call = callTo(fetchMock, '/projects')
    expect(call?.url).toContain(`date=${TEST_TODAY}`)
    expect(call?.url).toContain('archived=1')
    expect(call?.url).toContain('category=c1')
    expect(call?.url).toContain('q=')
  })

  it('sends If-Match on guarded writes', async () => {
    const fetchMock = stubFetch({
      '/api/v1/projects/p-1': { method: 'PATCH', body: makeProject({ version: 2 }) },
    })
    await api.updateProject('p-1', { name: 'new' }, '"v1"')
    const call = fetchMock.mock.calls.find((c) => (c[1]?.method ?? '') === 'PATCH')
    expect(call).toBeDefined()
    const headers = (call?.[1]?.headers ?? {}) as Record<string, string>
    expect(headers['If-Match']).toBe('"v1"')
  })

  it('unwraps list envelopes', async () => {
    stubFetch({
      '/api/v1/projects/p-1/checkins': { checkins: [{ id: 'c1' }] },
      '/api/v1/projects/p-1/skips': { skips: [] },
      '/api/v1/categories': { categories: [] },
    })
    await expect(api.listCheckins('p-1')).resolves.toHaveLength(1)
    await expect(api.listSkips('p-1')).resolves.toHaveLength(0)
    await expect(api.listCategories()).resolves.toHaveLength(0)
  })
})

describe('loadBootstrap', () => {
  it('fetches session, projects, categories and overview together', async () => {
    const fetchMock = stubFetch({
      '/api/v1/auth/me': { role: 'admin', username: 'admin' },
      '/api/v1/projects': { projects: [makeProject()], today: TEST_TODAY },
      '/api/v1/categories': { categories: [] },
      '/api/v1/stats/overview': makeOverview(),
    })
    const bootstrap = await loadBootstrap()
    expect(bootstrap.session.role).toBe('admin')
    expect(bootstrap.projects).toHaveLength(1)
    expect(bootstrap.today).toBe(TEST_TODAY)
    expect(fetchMock).toHaveBeenCalledTimes(4)
    // The client's own calendar day is sent explicitly.
    const call = callTo(fetchMock, '/projects')
    expect(call?.url).toMatch(/date=\d{4}-\d{2}-\d{2}/)
  })
})
