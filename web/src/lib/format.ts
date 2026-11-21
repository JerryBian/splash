/**
 * Display formatting. Kept in one module so a change in how the app talks
 * about numbers, time or schedules is a one-file edit, and so the functions
 * can be unit tested without rendering anything.
 */

import type { Project, ScheduleType } from '../api/types'
import { formatRangeCN, weekdayName } from './date'

/**
 * Renders a number without trailing zeros: 8 → "8", 5.5 → "5.5", 1234 → "1,234".
 * Values in this app are user-entered quantities, not measurements, so at most
 * one decimal place is shown.
 */
export function formatValue(n: number): string {
  if (!Number.isFinite(n)) return '0'
  const rounded = Math.round(n * 10) / 10
  return rounded.toLocaleString('zh-CN', { maximumFractionDigits: 1 })
}

/** "82%" from a 0..1 rate. */
export function formatPercent(rate: number): string {
  if (!Number.isFinite(rate)) return '0%'
  return `${Math.round(rate * 100)}%`
}

/**
 * Renders a duration in the largest unit that keeps two significant digits,
 * e.g. "42秒", "5分钟", "3小时", "2天". Used by the service panel.
 */
export function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return '0秒'
  if (seconds < 60) return `${Math.round(seconds)}秒`
  if (seconds < 3600) return `${Math.round(seconds / 60)}分钟`
  if (seconds < 86400) return `${Math.round(seconds / 3600)}小时`
  return `${Math.round(seconds / 86400)}天`
}

/**
 * Renders a timestamp as a short human phrase relative to now — "刚刚",
 * "12 分钟前", "3 天前" — falling back to an absolute date once it is older
 * than a week, where "47 天前" stops being useful.
 */
export function formatRelativeTime(iso: string, now: Date = new Date()): string {
  const then = new Date(iso)
  if (Number.isNaN(then.getTime())) return '未知时间'

  const deltaMs = now.getTime() - then.getTime()
  const magnitude = Math.abs(deltaMs)
  const minute = 60000
  const hour = 60 * minute
  const day = 24 * hour

  if (magnitude < 45 * 1000) return '刚刚'
  if (magnitude < hour) return `${Math.round(magnitude / minute)} 分钟前`
  if (magnitude < day) return `${Math.round(magnitude / hour)} 小时前`
  if (magnitude < 7 * day) return `${Math.round(magnitude / day)} 天前`
  return `${then.getMonth() + 1}月${then.getDate()}日`
}

/** "2026年11月21日 14:41" */
export function formatAbsoluteDate(date: Date): string {
  return `${date.getFullYear()}年${date.getMonth() + 1}月${date.getDate()}日 ${pad2(date.getHours())}:${pad2(date.getMinutes())}`
}

function pad2(n: number): string {
  return String(n).padStart(2, '0')
}

/** Parses an RFC 3339 timestamp, returning null for anything unparseable. */
export function parseTimestamp(iso: string): Date | null {
  const date = new Date(iso)
  return Number.isNaN(date.getTime()) ? null : date
}

/** Shortens a UUID to something a human can read aloud: "01890f3e…c2a1". */
export function shortId(id: string): string {
  if (id.length <= 13) return id
  return `${id.slice(0, 8)}…${id.slice(-4)}`
}

const SCHEDULE_NAMES: Record<ScheduleType, string> = {
  daily: '每天',
  weekdays: '每周指定日',
  weekly: '每周',
  monthly: '每月',
  interval_days: '每隔几天',
  interval_hours: '每隔几小时',
}

/** The schedule's family name, for pickers and headers. */
export function scheduleName(schedule: ScheduleType): string {
  return SCHEDULE_NAMES[schedule] ?? schedule
}

/**
 * One line describing a project's obligation, e.g. "每天 8 杯",
 * "每周一、三、五 1 次", "每 2 小时". Mirrors the server's schedule semantics.
 */
export function scheduleLabel(p: Pick<Project, 'schedule' | 'target' | 'unit' | 'weekdays' | 'interval'>): string {
  const goal = `${formatValue(p.target)} ${p.unit}`.trim()
  switch (p.schedule) {
    case 'daily':
      return `每天 ${goal}`
    case 'weekdays': {
      const days = [...p.weekdays].toSorted((a, b) => a - b).map(weekdayName).join('、')
      return `每周${days} ${goal}`
    }
    case 'weekly':
      return `每周 ${goal}`
    case 'monthly':
      return `每月 ${goal}`
    case 'interval_days':
      return `每 ${formatValue(p.interval)} 天 ${goal}`
    case 'interval_hours':
      return `每 ${formatValue(p.interval)} 小时`
    default:
      return ''
  }
}

/** The current period phrased for humans: "今天", "本周", "11月", "11月19日 – 11月21日". */
export function periodLabel(p: Pick<Project, 'schedule'>, periodStart: string, periodEnd: string, today: string): string {
  switch (p.schedule) {
    case 'daily':
    case 'weekdays':
      return periodStart === today ? '今天' : formatRangeCN(periodStart, periodEnd)
    case 'weekly':
      return periodStart === weekStartOf(today) ? '本周' : formatRangeCN(periodStart, periodEnd)
    case 'monthly':
      return periodStart.slice(0, 7) === today.slice(0, 7) ? '本月' : `${Number(periodStart.slice(5, 7))}月`
    case 'interval_days':
      return formatRangeCN(periodStart, periodEnd)
    case 'interval_hours':
      return '今天'
    default:
      return formatRangeCN(periodStart, periodEnd)
  }
}

function weekStartOf(day: string): string {
  const [y, m, d] = day.split('-').map(Number)
  const date = new Date(y ?? 0, (m ?? 1) - 1, d ?? 1)
  const wd = date.getDay() === 0 ? 7 : date.getDay()
  date.setDate(date.getDate() - (wd - 1))
  return `${date.getFullYear()}-${pad2(date.getMonth() + 1)}-${pad2(date.getDate())}`
}

/** Progress text "5 / 8 杯"; interval projects show a count instead. */
export function progressLabel(p: Pick<Project, 'schedule' | 'unit'>, value: number, target: number): string {
  if (p.schedule === 'interval_hours' || target <= 0) {
    return `${formatValue(value)} 次`
  }
  return `${formatValue(value)} / ${formatValue(target)} ${p.unit}`
}
