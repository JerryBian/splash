import { describe, expect, it } from 'vitest'
import {
  formatDuration,
  formatPercent,
  formatRelativeTime,
  formatValue,
  periodLabel,
  progressLabel,
  scheduleLabel,
  scheduleName,
} from './format'

describe('formatValue', () => {
  it('drops trailing zeros and groups thousands', () => {
    expect(formatValue(8)).toBe('8')
    expect(formatValue(5.5)).toBe('5.5')
    expect(formatValue(5.25)).toBe('5.3')
    expect(formatValue(1234)).toBe('1,234')
    expect(formatValue(Number.NaN)).toBe('0')
  })
})

describe('formatPercent', () => {
  it('rounds a 0..1 rate', () => {
    expect(formatPercent(0.826)).toBe('83%')
    expect(formatPercent(0)).toBe('0%')
    expect(formatPercent(Number.NaN)).toBe('0%')
  })
})

describe('formatDuration', () => {
  it('picks the largest sensible unit', () => {
    expect(formatDuration(0)).toBe('0秒')
    expect(formatDuration(42)).toBe('42秒')
    expect(formatDuration(300)).toBe('5分钟')
    expect(formatDuration(7200)).toBe('2小时')
    expect(formatDuration(172800)).toBe('2天')
  })
})

const iso = (d: Date) => d.toISOString()

describe('formatRelativeTime', () => {
  // Local-component dates on both sides: the assertions must hold in any
  // runner timezone, not just the one this file was written in.
  const now = new Date(2026, 10, 21, 12, 0)
  it('speaks in short Chinese phrases', () => {
    expect(formatRelativeTime(iso(new Date(2026, 10, 21, 11, 59, 30)), now)).toBe('刚刚')
    expect(formatRelativeTime(iso(new Date(2026, 10, 21, 11, 48)), now)).toBe('12 分钟前')
    expect(formatRelativeTime(iso(new Date(2026, 10, 21, 9, 0)), now)).toBe('3 小时前')
    expect(formatRelativeTime(iso(new Date(2026, 10, 18, 12, 0)), now)).toBe('3 天前')
  })
  it('falls back to an absolute date beyond a week', () => {
    expect(formatRelativeTime(iso(new Date(2026, 10, 1, 12, 0)), now)).toBe('11月1日')
  })
  it('survives garbage', () => {
    expect(formatRelativeTime('nope', now)).toBe('未知时间')
  })
})

describe('scheduleLabel', () => {
  const base = { target: 1, unit: '次', weekdays: [] as number[], interval: 0, nature: 'good' as const, mode: 'value' as const, targetTime: '' }
  it('describes every schedule family', () => {
    expect(scheduleLabel({ ...base, schedule: 'daily', target: 8, unit: '杯' })).toBe('每天 8 杯')
    expect(scheduleLabel({ ...base, schedule: 'weekdays', weekdays: [5, 1, 3] })).toBe('每周一、三、五 1 次')
    expect(scheduleLabel({ ...base, schedule: 'weekly', target: 3 })).toBe('每周 3 次')
    expect(scheduleLabel({ ...base, schedule: 'monthly', target: 4 })).toBe('每月 4 次')
    expect(scheduleLabel({ ...base, schedule: 'interval_days', interval: 3 })).toBe('每 3 天 1 次')
    expect(scheduleLabel({ ...base, schedule: 'interval_hours', interval: 2 })).toBe('每 2 小时')
  })
  it('phrases a bad habit as avoidance', () => {
    expect(scheduleLabel({ ...base, schedule: 'daily', nature: 'bad' })).toBe('每天 避免')
    expect(scheduleLabel({ ...base, schedule: 'weekly', nature: 'bad' })).toBe('每周 避免')
  })
  it('phrases a time habit with its deadline', () => {
    expect(scheduleLabel({ ...base, schedule: 'daily', mode: 'time', targetTime: '23:00' })).toBe('每天 23:00 前')
    expect(scheduleLabel({ ...base, schedule: 'daily', mode: 'time', targetTime: '' })).toBe('每天 记录时间')
  })
  it('names the families for pickers', () => {
    expect(scheduleName('daily')).toBe('每天')
    expect(scheduleName('interval_hours')).toBe('每隔几小时')
  })
})

describe('periodLabel', () => {
  it('says 今天/本周/本月 for the current period', () => {
    expect(periodLabel({ schedule: 'daily' }, '2026-11-21', '2026-11-21', '2026-11-21')).toBe('今天')
    expect(periodLabel({ schedule: 'weekly' }, '2026-11-16', '2026-11-22', '2026-11-21')).toBe('本周')
    expect(periodLabel({ schedule: 'monthly' }, '2026-11-01', '2026-11-30', '2026-11-21')).toBe('本月')
  })
  it('dates past periods', () => {
    expect(periodLabel({ schedule: 'weekly' }, '2026-11-09', '2026-11-15', '2026-11-21')).toBe('11月9日 – 11月15日')
    expect(periodLabel({ schedule: 'monthly' }, '2026-10-01', '2026-10-31', '2026-11-21')).toBe('10月')
  })
})

describe('progressLabel', () => {
  const base = { nature: 'good' as const, mode: 'value' as const }
  it('shows value over target with the unit', () => {
    expect(progressLabel({ ...base, schedule: 'daily', unit: '杯' }, 5, 8)).toBe('5 / 8 杯')
  })
  it('shows a plain count for interval projects', () => {
    expect(progressLabel({ ...base, schedule: 'interval_hours', unit: '次' }, 3, 0)).toBe('3 次')
  })
  it('shows a lapse count for a bad habit', () => {
    expect(progressLabel({ ...base, schedule: 'daily', unit: '次', nature: 'bad' }, 2, 1)).toBe('2 次')
  })
  it('only says whether a time habit was recorded', () => {
    expect(progressLabel({ ...base, schedule: 'daily', unit: '次', mode: 'time' }, 0, 1)).toBe('待记录')
    expect(progressLabel({ ...base, schedule: 'daily', unit: '次', mode: 'time' }, 1, 1)).toBe('已记录')
  })
})
