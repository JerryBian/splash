import type { Project } from '../api/types'
import { clockMinutes } from './schedule'

/**
 * Pure daily-reminder logic, kept out of the hook so the timing rules are
 * table-testable. A reminder is due for a grace window after its time rather
 * than only in the exact minute: a background tab throttles timers, and a
 * nudge that silently never fires is worse than one a few minutes late.
 */

/** How late a reminder may still fire, in minutes. */
export const REMINDER_GRACE_MINUTES = 30

const STORAGE_KEY = 'splash.reminders'

/** The local calendar day of an instant, YYYY-MM-DD. */
export function reminderDayKey(date: Date): string {
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${date.getFullYear()}-${month}-${day}`
}

/** Stable "reminded" key: one per project per local day. */
export function reminderKey(projectId: string, day: string): string {
  return `${day}:${projectId}`
}

/** A project that already met the day needs no nudge. */
function resolvedToday(status: string | undefined): boolean {
  return status === 'done' || status === 'skipped' || status === 'rest'
}

export type DueReminder = { project: Project; key: string }

/**
 * The reminders that should fire at `now`. Skips archived projects, projects
 * already resolved today (done/skipped/rest), the same reminder fired earlier
 * today, and anything outside the grace window.
 */
export function dueReminders(projects: Project[], now: Date, fired: ReadonlySet<string>): DueReminder[] {
  const minutes = now.getHours() * 60 + now.getMinutes()
  const day = reminderDayKey(now)
  const out: DueReminder[] = []
  for (const project of projects) {
    if (project.archived || project.reminderTime === '') continue
    const target = clockMinutes(project.reminderTime)
    if (target === null) continue
    if (minutes < target || minutes - target > REMINDER_GRACE_MINUTES) continue
    if (resolvedToday(project.status?.status)) continue
    const key = reminderKey(project.id, day)
    if (fired.has(key)) continue
    out.push({ project, key })
  }
  return out
}

/** Loads the persisted "already reminded" keys, tolerating corrupt storage. */
export function loadReminderKeys(): string[] {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY)
    if (raw === null) return []
    const parsed: unknown = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed.filter((value): value is string => typeof value === 'string') : []
  } catch {
    return []
  }
}

/**
 * Persists the keys that fired. Callers pass only today's keys, so the log is
 * naturally pruned when the day rolls over instead of growing forever.
 */
export function saveReminderKeys(keys: Iterable<string>): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify([...keys]))
  } catch {
    // Best effort: a missed write repeats at most one toast after a reload.
  }
}
