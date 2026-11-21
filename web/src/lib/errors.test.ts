import { describe, expect, it } from 'vitest'
import { ApiError, NetworkError } from '../api/client'
import { describeError, fieldErrorsFrom, translateFieldMessage } from './errors'

function apiError(status: number, code: string, details?: { field: string; message: string }[]): ApiError {
  return new ApiError(status, { code, message: 'raw english message', details })
}

describe('describeError', () => {
  it('speaks Chinese for every stable error code', () => {
    expect(describeError(apiError(401, 'unauthorized'))).toBe('需要管理员登录后才能操作。')
    expect(describeError(apiError(429, 'rate_limited'))).toBe('尝试次数过多，请几分钟后再试。')
    expect(describeError(apiError(404, 'not_found'))).toBe('内容不存在，或当前账号无权查看。')
    expect(describeError(apiError(409, 'conflict'))).toBe('名称已存在，或与当前状态冲突。')
    expect(describeError(apiError(412, 'precondition_failed'))).toBe('内容已被其他会话修改，请刷新后重试。')
    expect(describeError(apiError(500, 'internal_error'))).toBe('服务器内部错误，请稍后重试。')
  })

  it('never leaks the raw English server sentence for known codes', () => {
    const text = describeError(apiError(422, 'validation_failed', [{ field: 'name', message: 'is required' }]))
    expect(text).not.toContain('raw english')
    expect(text).toContain('输入有误')
    expect(text).toContain('必填')
  })

  it('explains an unreachable server with the fix', () => {
    const text = describeError(new NetworkError('/api/v1/health', new TypeError('x')))
    expect(text).toContain('无法连接服务器')
    expect(text).toContain('go run ./cmd/server')
  })

  it('falls back to the message for unknown codes', () => {
    expect(describeError(apiError(418, 'teapot'))).toBe('raw english message')
  })
})

describe('fieldErrorsFrom', () => {
  it('translates and indexes by field, collapsing array indices', () => {
    const error = apiError(422, 'validation_failed', [
      { field: 'name', message: 'is required' },
      { field: 'weekdays[2]', message: 'must contain days 1 (Monday) through 7 (Sunday)' },
      { field: 'description', message: 'must be at most 500 characters, got 501' },
    ])
    const fields = fieldErrorsFrom(error)
    expect(fields['name']).toBe('必填')
    expect(fields['weekdays']).toBe('星期取值应为 1–7')
    expect(fields['description']).toBe('最多 500 个字符')
  })

  it('returns empty for non-API errors', () => {
    expect(fieldErrorsFrom(new Error('x'))).toEqual({})
  })
})

describe('translateFieldMessage', () => {
  it('covers the server vocabulary', () => {
    expect(translateFieldMessage('must be a date in YYYY-MM-DD form')).toBe('日期格式应为 YYYY-MM-DD')
    expect(translateFieldMessage('must not be in the future')).toBe('不能晚于今天')
    expect(translateFieldMessage('must be one of daily, weekdays')).toBe('取值不在允许范围内')
    expect(translateFieldMessage('must be between 0 and 1e+06')).toBe('数值超出允许范围')
    expect(translateFieldMessage('must be a whole number of days, 1..365')).toBe('天数需为 1–365 的整数')
  })

  it('passes unknown messages through untouched', () => {
    expect(translateFieldMessage('something new')).toBe('something new')
  })
})
