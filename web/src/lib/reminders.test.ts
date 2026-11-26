import { describe, expect, it } from 'vitest'
import type { Project } from '../api/types'
import { makeProject, makeStatus } from '../test/fixtures'
import {
  dueReminders,
  REMINDER_GRACE_MINUTES,
  reminderDayKey,
  reminderKey,
} from './reminders'

/** An instant on the fixture day, in the test machine's local zone. */
function at(hour: number, minute: number): Date {
  return new Date(2026, 10, 21, hour, minute)
}

function reminderProject(over: Partial<Project> = {}): Project {
  return makeProject({ reminderTime: '22:30', ...over })
}

describe('dueReminders', () => {
  it('fires inside the grace window and only once per day', () => {
    const project = reminderProject()
    const key = reminderKey(project.id, reminderDayKey(at(22, 30)))

    expect(dueReminders([project], at(22, 29), new Set())).toEqual([])
    expect(dueReminders([project], at(22, 30), new Set())).toEqual([{ project, key }])
    expect(dueReminders([project], at(22, 59), new Set())).toHaveLength(1)
    expect(dueReminders([project], at(23, 0), new Set())).toHaveLength(1) // exactly the grace
    expect(dueReminders([project], at(23, 1), new Set())).toHaveLength(0)
    expect(dueReminders([project], at(22, 35), new Set([key]))).toHaveLength(0)
  })

  it('leaves out projects that need no nudge', () => {
    const done = reminderProject({ id: 'a', status: makeStatus({ status: 'done' }) })
    const skipped = reminderProject({ id: 'b', status: makeStatus({ status: 'skipped' }) })
    const rest = reminderProject({ id: 'c', status: makeStatus({ status: 'rest' }) })
    const archived = reminderProject({ id: 'd', archived: true })
    const none = reminderProject({ id: 'e', reminderTime: '' })
    const pending = reminderProject({ id: 'f', status: makeStatus({ status: 'pending' }) })
    const due = dueReminders([done, skipped, rest, archived, none, pending], at(22, 35), new Set())
    expect(due.map((d) => d.project.id)).toEqual(['f'])
  })

  it('keys by the local day so yesterday’s toast does not mute today', () => {
    const project = reminderProject()
    const yesterday = reminderKey(project.id, '2026-11-20')
    expect(dueReminders([project], at(22, 35), new Set([yesterday]))).toHaveLength(1)
    expect(reminderDayKey(at(0, 5))).toBe('2026-11-21')
    expect(REMINDER_GRACE_MINUTES).toBeGreaterThan(0)
  })
})
