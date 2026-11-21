import { describe, expect, it } from 'vitest'
import {
  addDays,
  daysBetween,
  formatClock,
  formatCountdown,
  formatDayCN,
  formatMonthCN,
  formatRangeCN,
  formatWeekdayCN,
  localToday,
  monthStart,
  parseDay,
  tzOffset,
  weekStart,
  weekdayName,
  weekdayOf,
} from './date'

describe('localToday', () => {
  it('renders the local calendar day, zero-padded', () => {
    expect(localToday(new Date(2026, 10, 21, 23, 59))).toBe('2026-11-21')
    expect(localToday(new Date(2026, 0, 5, 0, 0))).toBe('2026-01-05')
  })

  it('follows the local calendar day, not the UTC day, in any timezone', () => {
    // Built from LOCAL components so the expectation holds on every runner
    // timezone (CI is UTC; dev machines may be UTC+8 or anything else).
    const early = new Date(2026, 10, 21, 0, 30)
    const late = new Date(2026, 10, 21, 23, 30)
    expect(localToday(early)).toBe('2026-11-21')
    expect(localToday(late)).toBe('2026-11-21')
    // Whenever the runner's offset makes the UTC day differ from the local
    // day, localToday must still report the local one. Unconditional form:
    // local == UTC exactly when UTC happens to be the local day.
    for (const d of [early, late]) {
      const utcDay = d.toISOString().slice(0, 10)
      expect(localToday(d) === utcDay).toBe(utcDay === '2026-11-21')
    }
  })
})

describe('parseDay', () => {
  it('rejects malformed and impossible dates', () => {
    expect(parseDay('2026-13-01')).toBeNull()
    expect(parseDay('2026-02-30')).toBeNull()
    expect(parseDay('yesterday')).toBeNull()
    expect(parseDay('2026-11-2')).toBeNull()
  })

  it('accepts a real date at local midnight', () => {
    const d = parseDay('2026-11-21')
    expect(d?.getFullYear()).toBe(2026)
    expect(d?.getMonth()).toBe(10)
    expect(d?.getDate()).toBe(21)
  })
})

describe('calendar arithmetic', () => {
  it('adds days across month and year boundaries', () => {
    expect(addDays('2026-11-30', 1)).toBe('2026-12-01')
    expect(addDays('2026-12-31', 1)).toBe('2027-01-01')
    expect(addDays('2026-11-01', -1)).toBe('2026-10-31')
  })

  it('maps weekdays to 1..7 with Monday first', () => {
    expect(weekdayOf('2026-11-16')).toBe(1) // Monday
    expect(weekdayOf('2026-11-21')).toBe(6) // Saturday
    expect(weekdayOf('2026-11-22')).toBe(7) // Sunday
    expect(weekdayName(1)).toBe('一')
    expect(weekdayName(7)).toBe('日')
  })

  it('finds the Monday of the week', () => {
    expect(weekStart('2026-11-21')).toBe('2026-11-16')
    expect(weekStart('2026-11-16')).toBe('2026-11-16')
    expect(weekStart('2026-11-22')).toBe('2026-11-16')
  })

  it('finds month bounds', () => {
    expect(monthStart('2026-11-21')).toBe('2026-11-01')
  })

  it('counts days between dates', () => {
    expect(daysBetween('2026-11-16', '2026-11-21')).toBe(5)
    expect(daysBetween('2026-11-21', '2026-11-16')).toBe(-5)
  })
})

describe('display formatting', () => {
  it('renders Chinese dates and ranges', () => {
    expect(formatDayCN('2026-11-21')).toBe('11月21日')
    expect(formatMonthCN('2026-11-21')).toBe('2026年11月')
    expect(formatWeekdayCN('2026-11-21')).toBe('周六')
    expect(formatRangeCN('2026-11-16', '2026-11-22')).toBe('11月16日 – 11月22日')
    expect(formatRangeCN('2026-11-21', '2026-11-21')).toBe('11月21日')
  })

  it('renders clocks from instants', () => {
    expect(formatClock('2026-11-21T08:30:00+08:00')).toMatch(/^\d{2}:\d{2}$/)
    expect(formatClock('garbage')).toBe('')
  })

  it('counts down and floors at zero', () => {
    const now = new Date('2026-11-21T12:00:00Z')
    expect(formatCountdown('2026-11-21T13:23:45Z', now)).toBe('01:23:45')
    expect(formatCountdown('2026-11-21T11:00:00Z', now)).toBe('00:00:00')
    expect(formatCountdown('garbage', now)).toBe('00:00:00')
  })

  it('reports the timezone offset in minutes east', () => {
    expect(typeof tzOffset(new Date())).toBe('number')
  })
})
