import type {
  ApiErrorBody,
  Bootstrap,
  Category,
  CategoryInput,
  Checkin,
  CheckinInput,
  ExportData,
  Health,
  ImportCounts,
  ListProjectsParams,
  Overview,
  Project,
  ProjectInput,
  ProjectPatch,
  ProjectStats,
  Readiness,
  Session,
  Skip,
  VersionInfo,
} from './types'
import { localToday } from '../lib/date'
import { traceFailed, traceSettled, traceStart } from './trace'

/** Versioned API root. Matches RoutePrefix in internal/api/api.go. */
const BASE_URL = '/api/v1'

/**
 * A non-2xx response, decoded from the API's error envelope.
 *
 * Parameter properties (`constructor(private readonly x: T)`) are deliberately
 * not used: tsconfig sets erasableSyntaxOnly, because the Vite transform strips
 * types without reading them. Assigning the fields explicitly keeps this class
 * valid under that transform and identical in behaviour.
 */
export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly details: readonly { field: string; message: string }[]
  readonly requestId: string | undefined

  constructor(status: number, body: ApiErrorBody) {
    super(body.message || `request failed with status ${status}`)
    this.name = 'ApiError'
    this.status = status
    this.code = body.code || 'unknown'
    this.details = body.details ?? []
    this.requestId = body.requestId
    // Without this, `err instanceof ApiError` is false when the output is
    // transpiled down to an ES5-style class.
    Object.setPrototypeOf(this, ApiError.prototype)
  }

  /** Field problems rendered as one sentence, for showing under a form. */
  get fieldSummary(): string {
    return this.details.map((d) => `${d.field} ${d.message}`).join(', ')
  }
}

/**
 * Thrown when the network itself failed, as opposed to the API answering.
 *
 * The underlying reason travels in the standard ES2022 `cause` slot, so
 * `error.cause` works and nothing here shadows a built-in field.
 */
export class NetworkError extends Error {
  constructor(url: string, cause: unknown) {
    super(`could not reach ${url}`, { cause })
    this.name = 'NetworkError'
    Object.setPrototypeOf(this, NetworkError.prototype)
  }
}

type RequestOptions = Omit<RequestInit, 'body'> & { body?: unknown }

/**
 * Called when a data request answers 401 — the idle session expired. The app
 * registers this to return to the login gate. The auth door (/auth/*) is
 * exempt: a failed login is the gate's own business, not a session expiry.
 */
let unauthorizedHandler: (() => void) | null = null

export function setUnauthorizedHandler(fn: (() => void) | null): void {
  unauthorizedHandler = fn
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { body, headers, ...init } = options
  const url = `${BASE_URL}${path}`
  const method = init.method ?? 'GET'
  const startedAt = performance.now()
  traceStart(method, path)

  let response: Response
  try {
    response = await fetch(url, {
      ...init,
      // The session rides in an HttpOnly cookie; same-origin requests carry it
      // automatically, but being explicit keeps proxies from stripping it.
      credentials: 'same-origin',
      headers: {
        Accept: 'application/json',
        ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
        ...headers,
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch (cause) {
    traceFailed(method, path, startedAt)
    throw new NetworkError(url, cause)
  }

  traceSettled(method, path, response.status, response.headers.get('X-Request-ID'), startedAt)

  if (response.status === 204) {
    return undefined as T
  }

  // Read as text first: an error page from a proxy in front of the service is
  // usually HTML, and JSON.parse would turn a clear "502 from nginx" into a
  // confusing syntax error.
  const text = await response.text()
  const parsed = text === '' ? null : (safeParse(text) as ApiErrorBody | T | null)

  if (!response.ok) {
    if (response.status === 401 && !path.startsWith('/auth/') && unauthorizedHandler !== null) {
      unauthorizedHandler()
    }
    const envelope = isApiErrorBody(parsed)
      ? parsed
      : { code: 'unexpected_response', message: describeStatus(response, text) }
    throw new ApiError(response.status, envelope)
  }

  return parsed as T
}

/**
 * Recognises the API's error body after safeParse has unwrapped the envelope.
 * Deliberately structural rather than a class check: the value came off the
 * wire and has no prototype we control.
 */
function isApiErrorBody(value: unknown): value is ApiErrorBody {
  if (typeof value !== 'object' || value === null) return false
  const candidate = value as Record<string, unknown>
  return typeof candidate.code === 'string' && typeof candidate.message === 'string'
}

function safeParse(text: string): unknown {
  try {
    const value: unknown = JSON.parse(text)
    // Error responses are nested as {"error":{...}}. Flattening here means every
    // caller sees one shape and only this function knows about the envelope.
    if (isWrappedError(value)) {
      return value.error
    }
    return value
  } catch {
    return null
  }
}

function isWrappedError(value: unknown): value is { error: unknown } {
  if (typeof value !== 'object' || value === null) return false
  return 'error' in value && typeof (value as { error: unknown }).error === 'object'
}

function describeStatus(response: Response, body: string): string {
  const snippet = body.trim().slice(0, 200)
  return snippet === ''
    ? `${response.status} ${response.statusText}`
    : `${response.status} ${response.statusText}: ${snippet}`
}

function projectsQuery(params: ListProjectsParams = {}): string {
  const search = new URLSearchParams()
  if (params.date) search.set('date', params.date)
  if (params.archived) search.set('archived', '1')
  if (params.category) search.set('category', params.category)
  if (params.q) search.set('q', params.q)
  const encoded = search.toString()
  return encoded === '' ? '' : `?${encoded}`
}

const enc = encodeURIComponent

/** The API surface, grouped so call sites read as `api.listProjects(...)`. */
export const api = {
  health: (): Promise<Health> => request<Health>('/health'),

  readiness: (): Promise<Readiness> => request<Readiness>('/health/ready'),

  version: (): Promise<VersionInfo> => request<VersionInfo>('/version'),

  me: (): Promise<Session> => request<Session>('/auth/me'),

  login: (username: string, password: string): Promise<Session> =>
    request<Session>('/auth/login', { method: 'POST', body: { username, password } }),

  logout: (): Promise<Session> => request<Session>('/auth/logout', { method: 'POST' }),

  listCategories: (): Promise<Category[]> =>
    request<{ categories: Category[] }>('/categories').then((r) => r.categories),

  createCategory: (input: CategoryInput): Promise<Category> =>
    request<Category>('/categories', { method: 'POST', body: input }),

  updateCategory: (id: string, input: CategoryInput): Promise<Category> =>
    request<Category>(`/categories/${enc(id)}`, { method: 'PATCH', body: input }),

  deleteCategory: (id: string): Promise<void> =>
    request<void>(`/categories/${enc(id)}`, { method: 'DELETE' }),

  listProjects: (params?: ListProjectsParams): Promise<{ projects: Project[]; today: string }> =>
    request<{ projects: Project[]; today: string }>(`/projects${projectsQuery(params)}`),

  getProject: (id: string, date?: string): Promise<Project> =>
    request<Project>(`/projects/${enc(id)}${date ? `?date=${enc(date)}` : ''}`),

  createProject: (input: ProjectInput): Promise<Project> =>
    request<Project>('/projects', { method: 'POST', body: input }),

  updateProject: (id: string, patch: ProjectPatch, ifMatch?: string): Promise<Project> =>
    request<Project>(`/projects/${enc(id)}`, {
      method: 'PATCH',
      body: patch,
      ...(ifMatch ? { headers: { 'If-Match': ifMatch } } : {}),
    }),

  deleteProject: (id: string, ifMatch?: string): Promise<void> =>
    request<void>(`/projects/${enc(id)}`, {
      method: 'DELETE',
      ...(ifMatch ? { headers: { 'If-Match': ifMatch } } : {}),
    }),

  listCheckins: (
    projectId: string,
    params: { from?: string; to?: string; limit?: number } = {},
  ): Promise<Checkin[]> => {
    const search = new URLSearchParams()
    if (params.from) search.set('from', params.from)
    if (params.to) search.set('to', params.to)
    if (params.limit !== undefined) search.set('limit', String(params.limit))
    const qs = search.toString()
    return request<{ checkins: Checkin[] }>(
      `/projects/${enc(projectId)}/checkins${qs === '' ? '' : `?${qs}`}`,
    ).then((r) => r.checkins)
  },

  createCheckin: (projectId: string, input: CheckinInput): Promise<Checkin> =>
    request<Checkin>(`/projects/${enc(projectId)}/checkins`, { method: 'POST', body: input }),

  /**
   * Same-origin URL for a check-in's proof photo. Used directly as an <img>
   * src: the session cookie rides along, and the server marks the bytes
   * immutable so the browser caches them after the first view.
   */
  checkinPhotoUrl: (id: string): string => `${BASE_URL}/checkins/${enc(id)}/photo`,

  deleteCheckin: (id: string): Promise<void> =>
    request<void>(`/checkins/${enc(id)}`, { method: 'DELETE' }),

  listSkips: (projectId: string): Promise<Skip[]> =>
    request<{ skips: Skip[] }>(`/projects/${enc(projectId)}/skips`).then((r) => r.skips),

  createSkip: (projectId: string, date: string, reason?: string): Promise<Skip> =>
    request<Skip>(`/projects/${enc(projectId)}/skips`, { method: 'POST', body: { date, reason } }),

  deleteSkip: (projectId: string, date: string): Promise<void> =>
    request<void>(`/projects/${enc(projectId)}/skips/${enc(date)}`, { method: 'DELETE' }),

  projectStats: (projectId: string, params: { weeks?: number; date?: string } = {}): Promise<ProjectStats> => {
    const search = new URLSearchParams()
    if (params.weeks !== undefined) search.set('weeks', String(params.weeks))
    if (params.date) search.set('date', params.date)
    const qs = search.toString()
    return request<ProjectStats>(`/projects/${enc(projectId)}/stats${qs === '' ? '' : `?${qs}`}`)
  },

  overview: (date?: string): Promise<Overview> =>
    request<Overview>(`/stats/overview${date ? `?date=${enc(date)}` : ''}`),

  /** Same-origin URL; navigating to it downloads the backup with the session cookie. */
  exportUrl: `${BASE_URL}/export`,

  /** Restores a backup additively (by id); existing data is never overwritten. */
  importData: (data: ExportData): Promise<ImportCounts> =>
    request<ImportCounts>('/import', { method: 'POST', body: data }),
}

/** An empty dashboard, used when nobody is signed in yet. */
function emptyOverview(today: string): Overview {
  return {
    today: { date: today, total: 0, done: 0, pending: 0, skipped: 0, due: 0, slipped: 0 },
    totals: { checkins: 0, activeProjects: 0, bestStreak: 0, monthCheckins: 0, slips: 0, monthSlips: 0 },
    activity: [],
    categories: [],
    leaders: [],
    trend: [],
    violations: 0,
    recentViolations: [],
  }
}

/**
 * Loads everything the app needs, gating on the session first.
 *
 * Every data route requires the administrator, so an anonymous bootstrap must
 * not fire the data requests (they would all 401). When nobody is signed in we
 * return empty data and the shell renders the login gate; once signed in, the
 * three reads run in parallel under one Suspense boundary. The client's
 * calendar day is sent explicitly so "today" is the user's day even when the
 * server sits in another timezone.
 */
export async function loadBootstrap(): Promise<Bootstrap> {
  const today = localToday()
  const session = await api.me()
  if (session.role !== 'admin') {
    return { session, projects: [], categories: [], overview: emptyOverview(today), today }
  }
  const [projectsResponse, categories, overview] = await Promise.all([
    api.listProjects({ date: today }),
    api.listCategories(),
    api.overview(today),
  ])
  return {
    session,
    projects: projectsResponse.projects,
    today: projectsResponse.today,
    categories,
    overview,
  }
}
