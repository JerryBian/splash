#!/usr/bin/env node
/**
 * End-to-end smoke test against a RUNNING splash server:
 *
 *   node scripts/smoke.mjs                 # http://127.0.0.1:8080
 *   node scripts/smoke.mjs http://host:port
 *   node scripts/smoke.mjs --rate          # also exercise the login limiter
 *
 * It creates its own category and projects, walks the whole flow (auth,
 * the login gate, good/bad habits, check-in, backfill, skip, undo, If-Match,
 * stats, export, SPA) and deletes everything it created. Safe to run against
 * a real instance; --rate is opt-in because it locks the admin login for the
 * limiter window.
 */

const args = process.argv.slice(2)
const rateTest = args.includes('--rate')
const BASE = (args.find((a) => !a.startsWith('--')) ?? 'http://127.0.0.1:8080').replace(/\/$/, '')

let cookie = ''
let passed = 0

function fail(message) {
  process.stderr.write(`[smoke] FAIL: ${message}\n`)
  process.exit(1)
}

function ok(message) {
  passed++
  process.stdout.write(`[smoke] ok: ${message}\n`)
}

async function call(method, path, { body, status = 200, auth = true, raw = false } = {}) {
  const headers = { Accept: 'application/json' }
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (auth && cookie !== '') headers.Cookie = cookie
  const response = await fetch(BASE + path, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    redirect: 'manual',
  })
  const setCookie = response.headers.get('set-cookie')
  if (setCookie !== null) cookie = setCookie.split(';')[0]
  const text = await response.text()
  if (response.status !== status) {
    fail(`${method} ${path}: expected ${status}, got ${response.status}: ${text.slice(0, 300)}`)
  }
  if (raw) return text
  return text === '' ? null : JSON.parse(text)
}

const today = new Date()
const pad = (n) => String(n).padStart(2, '0')
const dayKey = (d) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
const TODAY = dayKey(today)
const shift = (n) => {
  const d = new Date(today)
  d.setDate(d.getDate() + n)
  return dayKey(d)
}

const stamp = Date.now()
const categoryName = `smoke-${stamp}`
let categoryId = ''
let publicId = ''
let badId = ''
let checkinId = ''

try {
  // --- system ---
  const health = await call('GET', '/api/v1/health', { auth: false })
  if (health.status !== 'ok') fail('health not ok')
  ok('health')
  await call('GET', '/api/v1/health/ready', { auth: false })
  ok('readiness')

  // --- anonymous baseline ---
  const me = await call('GET', '/api/v1/auth/me', { auth: false })
  if (me.role !== 'anonymous') fail('expected anonymous before login')
  ok('anonymous me')

  // --- login ---
  await call('POST', '/api/v1/auth/login', {
    body: { username: 'admin', password: 'definitely-wrong' },
    status: 401,
    auth: false,
  })
  ok('wrong password refused')
  const session = await call('POST', '/api/v1/auth/login', {
    body: { username: 'admin', password: process.env.SPLASH_ADMIN_PASSWORD ?? 'splash123' },
    auth: false,
  })
  if (session.role !== 'admin' || cookie === '') fail('login did not yield an admin session')
  ok('login sets a session cookie')
  const meAdmin = await call('GET', '/api/v1/auth/me')
  if (meAdmin.role !== 'admin') fail('cookie did not authenticate')
  ok('cookie authenticates')

  // --- categories & projects ---
  const category = await call('POST', '/api/v1/categories', {
    body: { name: categoryName, icon: '🧪', color: 'teal' },
    status: 201,
  })
  categoryId = category.id
  ok('create category')

  const base = {
    categoryId,
    schedule: 'daily',
    target: 2,
    unit: 'times',
    startDate: shift(-7),
  }
  const pub = await call('POST', '/api/v1/projects', {
    body: { ...base, name: `smoke-good-${stamp}`, icon: '💧', color: 'blue' },
    status: 201,
  })
  publicId = pub.id
  const bad = await call('POST', '/api/v1/projects', {
    body: { ...base, name: `smoke-bad-${stamp}`, icon: '🦉', color: 'indigo', nature: 'bad', target: 1, unit: '次' },
    status: 201,
  })
  badId = bad.id
  if (bad.nature !== 'bad' || bad.mode !== 'check') fail('bad habit not normalized to check mode')
  ok('create good + bad projects')

  // --- the login gate: every data route requires the administrator ---
  await call('GET', '/api/v1/projects', { status: 401, auth: false })
  await call('GET', `/api/v1/projects/${publicId}`, { status: 401, auth: false })
  await call('GET', `/api/v1/projects/${publicId}/stats`, { status: 401, auth: false })
  await call('GET', '/api/v1/stats/overview', { status: 401, auth: false })
  ok('anonymous reads refused')
  await call('POST', `/api/v1/projects/${publicId}/checkins`, {
    body: { value: 1 },
    status: 401,
    auth: false,
  })
  ok('anonymous write refused')

  // --- bad habit: a lapse is a slip, not a check-in ---
  await call('POST', `/api/v1/projects/${badId}/checkins`, { body: { value: 1 }, status: 201 })
  const badStats = await call('GET', `/api/v1/projects/${badId}/stats?date=${TODAY}`)
  if (badStats.summary.status !== 'missed') fail(`bad habit after lapse: ${badStats.summary.status}, want missed`)
  const badOverview = await call('GET', `/api/v1/stats/overview?date=${TODAY}`)
  if (badOverview.totals.slips < 1) fail('overview did not count the slip')
  if (badOverview.today.slipped < 1) fail('overview today did not count the slip')
  ok('bad habit lapse counts as a slip')

  // --- check-in flow ---
  const first = await call('POST', `/api/v1/projects/${publicId}/checkins`, {
    body: { value: 1, note: 'smoke', mood: '🙂', tzOffset: -today.getTimezoneOffset() },
    status: 201,
  })
  checkinId = first.id
  let stats = await call('GET', `/api/v1/projects/${publicId}/stats?date=${TODAY}`)
  if (stats.summary.status !== 'partial') fail(`expected partial, got ${stats.summary.status}`)
  await call('POST', `/api/v1/projects/${publicId}/checkins`, { body: { value: 1 }, status: 201 })
  stats = await call('GET', `/api/v1/projects/${publicId}/stats?date=${TODAY}`)
  if (stats.summary.status !== 'done' || stats.summary.streak !== 1) {
    fail(`expected done/streak 1, got ${stats.summary.status}/${stats.summary.streak}`)
  }
  ok('check-in reaches the target and the streak')

  // Backfill yesterday → streak 2.
  await call('POST', `/api/v1/projects/${publicId}/checkins`, {
    body: { value: 2, date: shift(-1) },
    status: 201,
  })
  stats = await call('GET', `/api/v1/projects/${publicId}/stats?date=${TODAY}`)
  if (stats.summary.streak !== 2) fail(`backfill streak = ${stats.summary.streak}, want 2`)
  ok('backfill repairs the streak')

  // Future date refused.
  await call('POST', `/api/v1/projects/${publicId}/checkins`, {
    body: { value: 1, date: shift(2) },
    status: 422,
  })
  ok('future check-in refused')

  // --- skips ---
  await call('POST', `/api/v1/projects/${publicId}/skips`, {
    body: { date: shift(-2), reason: 'smoke' },
    status: 201,
  })
  await call('POST', `/api/v1/projects/${publicId}/skips`, {
    body: { date: shift(-2) },
    status: 409,
  })
  stats = await call('GET', `/api/v1/projects/${publicId}/stats?date=${TODAY}`)
  if (stats.totals.skipped !== 1) fail('skip not counted')
  await call('DELETE', `/api/v1/projects/${publicId}/skips/${shift(-2)}`, { status: 204 })
  ok('skip create / conflict / count / delete')

  // --- undo ---
  await call('DELETE', `/api/v1/checkins/${checkinId}`, { status: 204 })
  stats = await call('GET', `/api/v1/projects/${publicId}/stats?date=${TODAY}`)
  if (stats.summary.status !== 'partial') fail('undo did not revert to partial')
  ok('undo reverts the status')

  // --- optimistic concurrency ---
  await call('PATCH', `/api/v1/projects/${publicId}`, {
    body: { description: 'smoke edit' },
    headers: undefined,
    status: 200,
  })
  // The project is now v2; a stale v1 write must 412. Fetch the current ETag.
  const current = await call('GET', `/api/v1/projects/${publicId}`)
  const etag = `"v${current.version}"`
  const stale = await fetch(`${BASE}/api/v1/projects/${publicId}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json', Cookie: cookie, 'If-Match': '"v1"' },
    body: JSON.stringify({ description: 'stale' }),
  })
  if (stale.status !== 412) fail(`stale If-Match: expected 412, got ${stale.status}`)
  const fresh = await fetch(`${BASE}/api/v1/projects/${publicId}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json', Cookie: cookie, 'If-Match': etag },
    body: JSON.stringify({ description: 'fresh' }),
  })
  if (fresh.status !== 200) fail(`fresh If-Match: expected 200, got ${fresh.status}`)
  ok('If-Match: stale 412, fresh 200')

  // --- overview & export ---
  const overview = await call('GET', `/api/v1/stats/overview?date=${TODAY}`)
  if (overview.totals.checkins < 1) fail('overview saw no check-ins')
  ok('overview')
  const dump = await call('GET', '/api/v1/export', { raw: true })
  if (!dump.includes(publicId)) fail('export missing the project')
  ok('export contains the data')

  // --- cleanup ---
  await call('DELETE', `/api/v1/projects/${badId}`, { status: 204 })
  await call('GET', `/api/v1/projects/${badId}`, { status: 404 })
  await call('DELETE', `/api/v1/projects/${publicId}`, { status: 204 })
  await call('DELETE', `/api/v1/categories/${categoryId}`, { status: 204 })
  ok('delete cascades and cleans up')

  // --- SPA & error envelope ---
  const html = await call('GET', '/', { auth: false, raw: true })
  if (!html.includes('id="root"')) fail('SPA not embedded')
  ok('SPA embedded')
  const notFound = await call('GET', '/api/v1/does-not-exist', { status: 404, auth: false })
  if (notFound.error?.code !== 'not_found') fail('404 is not the JSON envelope')
  ok('unknown API path is JSON 404')

  // --- optional: login rate limit (locks the account for the window) ---
  if (rateTest) {
    let limited = false
    for (let i = 0; i < 15; i++) {
      const response = await fetch(`${BASE}/api/v1/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: 'admin', password: 'wrong' }),
      })
      if (response.status === 429) {
        limited = true
        break
      }
    }
    if (!limited) fail('login limiter never engaged')
    ok('login rate limit engages')
  }

  process.stdout.write(`[smoke] PASS — ${passed} checks against ${BASE}\n`)
} catch (error) {
  // Best-effort cleanup so a failed run does not leave smoke data behind.
  for (const [method, path] of [
    ['DELETE', `/api/v1/projects/${publicId}`],
    ['DELETE', `/api/v1/projects/${badId}`],
    ['DELETE', `/api/v1/categories/${categoryId}`],
  ]) {
    if (path.endsWith('/')) continue
    await fetch(BASE + path, { method, headers: { Cookie: cookie } }).catch(() => {})
  }
  fail(error.message)
}
