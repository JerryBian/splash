import { ApiError, NetworkError } from '../api/client'

/**
 * The API's error codes are a stable English contract (logs, scripts, CI);
 * the UI is Chinese. This map is the single place the two meet, so no screen
 * ever shows a raw server sentence.
 */
const CODE_MESSAGES: Record<string, string> = {
  unauthorized: '需要管理员登录后才能操作。',
  rate_limited: '尝试次数过多，请几分钟后再试。',
  not_found: '内容不存在，或当前账号无权查看。',
  conflict: '名称已存在，或与当前状态冲突。',
  precondition_failed: '内容已被其他会话修改，请刷新后重试。',
  validation_failed: '输入有误，请检查标红的字段。',
  payload_too_large: '提交内容过大。',
  invalid_json: '请求格式错误。',
  invalid_argument: '请求参数无效。',
  method_not_allowed: '该操作不被允许。',
  timeout: '请求超时，请重试。',
  internal_error: '服务器内部错误，请稍后重试。',
}

/**
 * Turns anything thrown by the API layer into one sentence a person can act on.
 *
 * Errors here are directions, not apologies: they say what happened and what to
 * do next. The most common failure is not a bug — it is the Go server not
 * running — so that case gets its own message with the command that fixes it.
 */
export function describeError(error: unknown): string {
  if (error instanceof NetworkError) {
    return '无法连接服务器。请先启动后端（go run ./cmd/server），然后重试。'
  }
  if (error instanceof ApiError) {
    const base = CODE_MESSAGES[error.code] ?? error.message
    const detail = translatedFieldSummary(error)
    if (detail !== '') return `${base}（${detail}）`
    return base
  }
  if (error instanceof Error) return error.message
  return String(error)
}

/**
 * Indexes an API error's field problems by field name, so a form can render
 * each one under the input it belongs to — translated, never raw English.
 */
export function fieldErrorsFrom(error: unknown): Record<string, string> {
  if (!(error instanceof ApiError)) return {}
  const out: Record<string, string> = {}
  for (const detail of error.details) {
    // The server reports array elements as "weekdays[2]"; the form has one
    // weekdays control, so the first problem for that field is the one to show.
    const field = detail.field.replace(/\[\d+\]$/, '')
    if (out[field] === undefined) out[field] = translateFieldMessage(detail.message)
  }
  return out
}

function translatedFieldSummary(error: ApiError): string {
  return error.details.map((d) => `${d.field} ${translateFieldMessage(d.message)}`).join('，')
}

/**
 * The server's field messages are a small closed vocabulary (see
 * internal/store/domain.go); translating them here keeps the UI monolingual
 * without weakening the API contract. Unknown messages pass through.
 */
export function translateFieldMessage(message: string): string {
  if (message === 'is required') return '必填'
  if (message === 'does not exist') return '不存在'
  if (message === 'must set at least one field') return '至少修改一个字段'
  if (message === 'must list at least one day') return '至少选择一天'
  if (message === 'must be a date in YYYY-MM-DD form') return '日期格式应为 YYYY-MM-DD'
  if (message === 'must not be in the future') return '不能晚于今天'
  if (message === 'must not be before the project start date') return '不能早于项目开始日期'
  if (message === 'must contain days 1 (Monday) through 7 (Sunday)') return '星期取值应为 1–7'
  if (message === 'must be a UUID or empty') return '分类标识无效'
  if (message === 'is not a supported mood') return '心情取值无效'
  if (message === 'must not be negative') return '不能为负数'
  if (message === 'must be an integer') return '需为整数'
  const atMost = message.match(/^must be at most (\d+) characters(?:, got \d+)?$/)
  if (atMost !== null) return `最多 ${atMost[1]} 个字符`
  if (message.startsWith('must be between')) return '数值超出允许范围'
  if (message.startsWith('must be one of')) return '取值不在允许范围内'
  if (message.startsWith('must be a whole number of days')) return '天数需为 1–365 的整数'
  if (message.startsWith('must be 1..')) return '间隔超出允许范围'
  if (message.startsWith('must be positive')) return '需大于 0'
  return message
}

/** True when the failure means "the server is not there" rather than "bad input". */
export function isUnreachable(error: unknown): boolean {
  return error instanceof NetworkError
}

/** True when the session expired or was never there; the UI should offer login. */
export function isUnauthorized(error: unknown): boolean {
  return error instanceof ApiError && error.status === 401
}
