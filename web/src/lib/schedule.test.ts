import { describe, expect, it } from 'vitest'
import { makeProject, makeStatus } from '../test/fixtures'
import {
  activityLevel,
  dailyEquivalentTarget,
  heatLevel,
  isBad,
  isCheckable,
  isComplete,
  needsValueSheet,
  postState,
  STATUS_LABEL,
  statusLabel,
} from './schedule'

describe('status helpers', () => {
  it('knows which statuses are complete and checkable', () => {
    expect(isComplete('done')).toBe(true)
    expect(isComplete('partial')).toBe(false)
    expect(isCheckable('pending')).toBe(true)
    expect(isCheckable('due')).toBe(true)
    expect(isCheckable('waiting')).toBe(true)
    expect(isCheckable('done')).toBe(false)
    expect(isCheckable('rest')).toBe(false)
  })
  it('labels every status in Chinese', () => {
    expect(STATUS_LABEL['done']).toBe('已完成')
    expect(STATUS_LABEL['missed']).toBe('缺失')
    expect(STATUS_LABEL['skipped']).toBe('已跳过')
  })
})

describe('nature-aware helpers', () => {
  it('labels the same verdict differently for a bad habit', () => {
    expect(statusLabel('done', 'good')).toBe('已完成')
    expect(statusLabel('done', 'bad')).toBe('未破戒')
    expect(statusLabel('missed', 'good')).toBe('缺失')
    expect(statusLabel('missed', 'bad')).toBe('已破戒')
    expect(statusLabel('done')).toBe('已完成') // defaults to good
  })
  it('fills clean days and empties lapse days for a bad habit', () => {
    const clean = { date: '2026-11-21', value: 0, count: 0, status: 'done' as const }
    const lapse = { date: '2026-11-20', value: 1, count: 1, status: 'missed' as const }
    expect(heatLevel(clean, 1, 'bad')).toBe(4)
    expect(heatLevel(lapse, 1, 'bad')).toBe(0)
    // A good habit still scales by magnitude.
    expect(heatLevel(clean, 8, 'good')).toBe(0)
  })
  it('knows a bad project', () => {
    expect(isBad(makeProject({ nature: 'bad' }))).toBe(true)
    expect(isBad(makeProject({ nature: 'good' }))).toBe(false)
  })
})

describe('dailyEquivalentTarget', () => {
  it('spreads period targets across days', () => {
    expect(dailyEquivalentTarget(makeProject({ schedule: 'daily', target: 8 }))).toBe(8)
    expect(dailyEquivalentTarget(makeProject({ schedule: 'weekly', target: 7 }))).toBe(1)
    expect(dailyEquivalentTarget(makeProject({ schedule: 'interval_days', target: 2, interval: 4 }))).toBe(0.5)
    expect(dailyEquivalentTarget(makeProject({ schedule: 'interval_hours', interval: 2 }))).toBe(12)
  })
  it('uses the current month length for monthly goals', () => {
    const p = makeProject({
      schedule: 'monthly',
      target: 30,
      status: makeStatus({ periodStart: '2026-11-01' }),
    })
    expect(dailyEquivalentTarget(p)).toBe(1) // November has 30 days
  })
})

const cell = (value: number) => ({ date: '2026-11-21', value, count: value > 0 ? 1 : 0, status: 'done' as const })

describe('heatLevel', () => {
  it('scales one hue across four steps', () => {
    expect(heatLevel(cell(0), 8)).toBe(0)
    expect(heatLevel(cell(1), 8)).toBe(1)
    expect(heatLevel(cell(3), 8)).toBe(2)
    expect(heatLevel(cell(6), 8)).toBe(3)
    expect(heatLevel(cell(8), 8)).toBe(4)
    expect(heatLevel(cell(20), 8)).toBe(4)
  })
})

describe('activityLevel', () => {
  it('scales by the busiest day', () => {
    expect(activityLevel(0, 10)).toBe(0)
    expect(activityLevel(1, 10)).toBe(1)
    expect(activityLevel(3, 10)).toBe(2)
    expect(activityLevel(6, 10)).toBe(3)
    expect(activityLevel(9, 10)).toBe(4)
    expect(activityLevel(5, 0)).toBe(0)
  })
})

describe('needsValueSheet', () => {
  it('opens the sheet for measured goals, quick-checks the rest', () => {
    expect(needsValueSheet(makeProject({ target: 8, unit: '杯' }))).toBe(true)
    expect(needsValueSheet(makeProject({ target: 30, unit: '分钟' }))).toBe(true)
    expect(needsValueSheet(makeProject({ target: 1, unit: '次' }))).toBe(false)
    expect(needsValueSheet(makeProject({ target: 3, unit: '次' }))).toBe(false)
    expect(needsValueSheet(makeProject({ schedule: 'interval_hours', interval: 2 }))).toBe(false)
  })
})

describe('postState', () => {
  it('reads pending, broken, resolved and waiting from the summary', () => {
    const source = makeProject({ postrequisites: ['p-2'] })
    expect(postState(source, makeStatus({ pendingPosts: ['p-2'] }), 'p-2')).toBe('pending')
    expect(postState(source, makeStatus({ brokenPosts: ['p-2'] }), 'p-2')).toBe('broken')
    expect(postState(source, makeStatus({ status: 'done', value: 1, target: 1 }), 'p-2')).toBe('resolved')
    expect(postState(source, makeStatus({ value: 0, target: 1 }), 'p-2')).toBe('waiting')
    const interval = makeProject({ schedule: 'interval_hours', interval: 2 })
    expect(postState(interval, makeStatus({ count: 1, target: 0 }), 'p-2')).toBe('resolved')
    expect(postState(interval, makeStatus({ count: 0, target: 0 }), 'p-2')).toBe('waiting')
  })
})
