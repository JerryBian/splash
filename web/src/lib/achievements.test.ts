import { describe, expect, it } from 'vitest'
import { makeProject, makeStatus } from '../test/fixtures'
import type { Totals } from '../api/types'
import { achievements } from './achievements'

function totals(over: Partial<Totals> = {}): Totals {
  return {
    checkins: 0, value: 0, activeDays: 0, done: 0, missed: 0, skipped: 0,
    completionRate: 0, onTimeRate: 0, onTimeCount: 0, lateCount: 0, backfilled: 0, violations: 0,
    postReqs: 0, postViolations: 0, commitmentRate: 0,
    ...over,
  }
}

describe('achievements', () => {
  it('marks streak, period and count milestones earned from derived stats', () => {
    const list = achievements(makeProject(), totals({ checkins: 60, done: 12 }), makeStatus({ bestStreak: 30 }))
    const byId = new Map(list.map((a) => [a.id, a]))
    expect(byId.get('streak-7')?.earned).toBe(true)
    expect(byId.get('streak-30')?.earned).toBe(true)
    expect(byId.get('streak-100')?.earned).toBe(false)
    expect(byId.get('period-10')?.earned).toBe(true)
    expect(byId.get('period-50')?.earned).toBe(false)
    expect(byId.get('count-50')?.earned).toBe(true)
    expect(byId.get('count-100')?.earned).toBe(false)
  })
  it('reports progress toward an unearned milestone', () => {
    const list = achievements(makeProject(), totals(), makeStatus({ bestStreak: 7 }))
    const streak30 = list.find((a) => a.id === 'streak-30')
    expect(streak30?.earned).toBe(false)
    expect(streak30?.progress).toBeCloseTo(7 / 30, 5)
  })
  it('uses 坚持 wording for bad habits', () => {
    const list = achievements(makeProject({ nature: 'bad' }), totals(), makeStatus())
    expect(list.find((a) => a.id === 'streak-7')?.label).toContain('坚持')
  })
  it('never rewards a bad habit for recording lapses', () => {
    // Lapses show up as checkins but must not earn a "打卡" badge.
    const list = achievements(makeProject({ nature: 'bad' }), totals({ checkins: 200 }), makeStatus())
    expect(list.some((a) => a.id.startsWith('count-'))).toBe(false)
    expect(list.find((a) => a.id === 'period-10')?.label).toContain('守住')
  })
  it('adds punctuality badges for interval projects', () => {
    const interval = makeProject({ schedule: 'interval_hours', interval: 2 })
    const list = achievements(interval, totals({ onTimeCount: 12 }), makeStatus())
    expect(list.find((a) => a.id === 'ontime-10')?.earned).toBe(true)
    expect(list.find((a) => a.id === 'ontime-30')?.earned).toBe(false)
    const still = achievements(makeProject({ schedule: 'daily' }), totals({ onTimeCount: 50 }), makeStatus())
    expect(still.some((a) => a.id.startsWith('ontime-'))).toBe(false)
  })
  it('rewards fulfilled postrequisite commitments only when there are some', () => {
    const linked = makeProject({ postrequisites: ['p-2'] })
    const list = achievements(linked, totals({ postReqs: 12, postViolations: 2 }), makeStatus())
    expect(list.find((a) => a.id === 'link-10')?.earned).toBe(true)
    expect(list.find((a) => a.id === 'link-30')?.earned).toBe(false)
    const unlinked = achievements(makeProject(), totals({ postReqs: 50 }), makeStatus())
    expect(unlinked.some((a) => a.id.startsWith('link-'))).toBe(false)
  })
})
