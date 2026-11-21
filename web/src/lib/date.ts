/**
 * Calendar-day helpers. Every date in the app is a YYYY-MM-DD string in the
 * user's own timezone, matching the server's local_date convention: period
 * arithmetic is about the user's day, not the server's.
 *
 * All functions are pure and injectable-now so tests are deterministic.
 */

/** The user's calendar day, YYYY-MM-DD. */
export function localToday(now: Date = new Date()): string {
  const y = now.getFullYear()
  const m = String(now.getMonth() + 1).padStart(2, '0')
  const d = String(now.getDate()).padStart(2, '0')
  return `${y}-${m}-${d}`
}

/** Minutes east of UTC, the sign convention the API expects. */
export function tzOffset(now: Date = new Date()): number {
  return -now.getTimezoneOffset()
}

/** Parses YYYY-MM-DD at local midnight; null for anything unparseable. */
export function parseDay(day: string): Date | null {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(day)) return null
  const [y, m, d] = day.split('-').map(Number)
  const date = new Date(y ?? 0, (m ?? 1) - 1, d ?? 1)
  if (date.getFullYear() !== y || date.getMonth() !== (m ?? 1) - 1 || date.getDate() !== d) {
    return null
  }
  return date
}

export function dayKey(date: Date): string {
  return localToday(date)
}

export function addDays(day: string, n: number): string {
  const date = parseDay(day)
  if (date === null) return day
  date.setDate(date.getDate() + n)
  return dayKey(date)
}

/** 1 (Monday) .. 7 (Sunday), the server's convention. */
export function weekdayOf(day: string): number {
  const date = parseDay(day)
  if (date === null) return 1
  const wd = date.getDay()
  return wd === 0 ? 7 : wd
}

/** The Monday of day's week. */
export function weekStart(day: string): string {
  return addDays(day, -(weekdayOf(day) - 1))
}

export function monthStart(day: string): string {
  return `${day.slice(0, 7)}-01`
}

export function daysBetween(a: string, b: string): number {
  const da = parseDay(a)
  const db = parseDay(b)
  if (da === null || db === null) return 0
  return Math.round((db.getTime() - da.getTime()) / 86400000)
}

const WEEKDAY_NAMES = ['一', '二', '三', '四', '五', '六', '日'] as const

/** 周一..周日 for a 1..7 weekday number. */
export function weekdayName(wd: number): string {
  return WEEKDAY_NAMES[wd - 1] ?? '?'
}

/** "11月21日" */
export function formatDayCN(day: string): string {
  const date = parseDay(day)
  if (date === null) return day
  return `${date.getMonth() + 1}月${date.getDate()}日`
}

/** "2026年11月" */
export function formatMonthCN(day: string): string {
  const date = parseDay(day)
  if (date === null) return day
  return `${date.getFullYear()}年${date.getMonth() + 1}月`
}

/** "11月" — compact axis label. */
export function formatMonthShort(day: string): string {
  const date = parseDay(day)
  if (date === null) return day
  return `${date.getMonth() + 1}月`
}

/** "周五" */
export function formatWeekdayCN(day: string): string {
  return `周${weekdayName(weekdayOf(day))}`
}

/** "11月16日 – 11月22日", a period label. */
export function formatRangeCN(start: string, end: string): string {
  if (start === end) return formatDayCN(start)
  return `${formatDayCN(start)} – ${formatDayCN(end)}`
}

/** "08:30" from an RFC 3339 instant, in the user's timezone. */
export function formatClock(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return ''
  return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`
}

/** Countdown "01:23:45" until an instant; "00:00:00" once elapsed. */
export function formatCountdown(targetIso: string, now: Date = new Date()): string {
  const target = new Date(targetIso)
  if (Number.isNaN(target.getTime())) return '00:00:00'
  let seconds = Math.max(0, Math.floor((target.getTime() - now.getTime()) / 1000))
  const h = Math.floor(seconds / 3600)
  seconds -= h * 3600
  const m = Math.floor(seconds / 60)
  const s = seconds - m * 60
  return `${pad2(h)}:${pad2(m)}:${pad2(s)}`
}

function pad2(n: number): string {
  return String(n).padStart(2, '0')
}
