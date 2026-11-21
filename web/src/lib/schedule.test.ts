import { describe, expect, it } from 'vitest'
import { makeProject, makeStatus } from '../test/fixtures'
import {
  activityLevel,
  dailyEquivalentTarget,
  heatLevel,
  isCheckable,
  isComplete,
  needsValueSheet,
  STATUS_LABEL,
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
