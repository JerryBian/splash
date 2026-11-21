import { vi } from 'vitest'

/**
 * A stand-in for the Response objects the real fetch returns.
 *
 * Built by hand rather than with `new Response(...)` because the jsdom test
 * environment does not guarantee the Fetch API globals, and the client only
 * ever touches status, headers.get and text.
 */
export function fakeResponse(
  status: number,
  body: unknown,
  headers: Record<string, string> = {},
): Response {
  const lowered = new Map<string, string>()
  for (const [key, value] of Object.entries(headers)) {
    lowered.set(key.toLowerCase(), value)
  }
  const text = typeof body === 'string' ? body : body === null ? '' : JSON.stringify(body)

  return {
    status,
    statusText: String(status),
    ok: status >= 200 && status < 300,
    headers: {
      get: (name: string) => lowered.get(name.toLowerCase()) ?? null,
    },
    text: () => Promise.resolve(text),
  } as unknown as Response
}

/** A response to serve: an explicit status, body and/or HTTP method filter. */
export type RouteDescriptor = { status?: number; body?: unknown; method?: string }

export type RouteMap = Record<string, RouteDescriptor | RouteDescriptor[] | unknown>

export type FetchMock = ReturnType<
  typeof vi.fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>
>

/**
 * Installs a fetch stub that answers by URL prefix.
 *
 * A route value is served as a 200 body, unless it has a `body` property, which
 * marks it as an explicit descriptor:
 *
 *	stubFetch({
 *	  '/api/v1/health': healthPayload,                     // 200 + payload
 *	  '/api/v1/projects': { status: 201, body: created },  // 201 + payload
 *	})
 *
 * An array of descriptors is tried in order; a descriptor with a `method`
 * only matches that method, which is how one prefix serves both the POST
 * create and the GET list:
 *
 *	'/api/v1/projects': [
 *	  { method: 'POST', status: 201, body: created },
 *	  { body: { projects: [], today: '2026-11-21' } },
 *	]
 *
 * The marker is `body`, not `status`, on purpose: this API has payloads with a
 * `status` field of their own (/api/v1/health answers {"status":"ok"}), and
 * sniffing on that turned a healthy response into a bogus one.
 *
 * Anything unmatched returns the API's 404 envelope, so a test that requests
 * the wrong path fails with a recognisable error instead of hanging.
 */
export function stubFetch(routes: RouteMap): FetchMock {
  const fetchMock = vi.fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      const path = url.replace(/^https?:\/\/[^/]+/, '')
      const method = (init?.method ?? 'GET').toUpperCase()

      for (const [prefix, value] of Object.entries(routes)) {
        if (!path.startsWith(prefix)) continue
        const candidates = Array.isArray(value) ? value : [value]
        for (const candidate of candidates) {
          if (isDescriptor(candidate)) {
            if (candidate.method !== undefined && candidate.method.toUpperCase() !== method) continue
            return fakeResponse(candidate.status ?? 200, candidate.body ?? null)
          }
          return fakeResponse(200, candidate)
        }
      }

      return fakeResponse(404, {
        error: { code: 'not_found', message: `no route for ${method} ${path}` },
      })
    },
  )

  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function isDescriptor(value: unknown): value is RouteDescriptor {
  // The marker is `body`/`method`, never `status`: payloads like /health carry
  // their own `status` field, and sniffing on it turns a healthy response into
  // a bogus descriptor.
  return typeof value === 'object' && value !== null && ('body' in value || 'method' in value)
}

/** Finds the first call to a path prefix with the given method. */
export function callTo(
  fetchMock: FetchMock,
  prefix: string,
  method = 'GET',
): { url: string; body: unknown } | undefined {
  for (const call of fetchMock.mock.calls) {
    const url = String(call[0])
    const used = (call[1]?.method ?? 'GET').toUpperCase()
    if (url.includes(prefix) && used === method.toUpperCase()) {
      const raw = call[1]?.body
      return { url, body: typeof raw === 'string' ? JSON.parse(raw) : raw }
    }
  }
  return undefined
}
