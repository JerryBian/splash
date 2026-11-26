/**
 * Client-side reading of the server's schedule verdicts: labels, heat levels
 * and the rules that decide what a tap does. The server computes every status;
 * nothing here re-derives streaks or periods, so the two ends cannot disagree.
 */

import type { DayStatus, HeatCell, Project, ProjectNature } from '../api/types'
import { parseDay } from './date'

/** True when the project records a wall-clock time per check-in. */
export function isTime(p: Pick<Project, 'mode'>): boolean {
  return p.mode === 'time'
}

/** "HH:MM" as minutes since midnight, or null when the string is malformed. */
export function clockMinutes(clock: string): number | null {
  if (!/^\d{2}:\d{2}$/.test(clock)) return null
  const hours = Number(clock.slice(0, 2))
  const minutes = Number(clock.slice(3, 5))
  if (hours > 23 || minutes > 59) return null
  return hours * 60 + minutes
}

/**
 * True when a time-mode record landed after the project's deadline.
 *
 * Mirrors store.LateForDeadline: the comparison is circular over a ±12h window
 * around the deadline, not a same-day string compare. "Asleep by 00:30" logged
 * at 23:40 is early (the bedtime run starts in the evening), while a 23:00
 * deadline logged at 00:10 is late.
 */
export function isLate(p: Pick<Project, 'mode' | 'targetTime'>, c: { time?: string }): boolean {
  if (p.mode !== 'time' || p.targetTime === '' || c.time === undefined || c.time === '') return false
  const target = clockMinutes(p.targetTime)
  const at = clockMinutes(c.time)
  if (target === null || at === null) return false
  const diff = (at - target + 24 * 60) % (24 * 60)
  return diff > 0 && diff <= 12 * 60
}

/** True when the project tracks a habit to avoid rather than one to build. */
export function isBad(p: Pick<Project, 'nature'>): boolean {
  return p.nature === 'bad'
}

export const STATUS_LABEL: Record<DayStatus, string> = {
  done: '已完成',
  partial: '进行中',
  pending: '待打卡',
  missed: '缺失',
  skipped: '已跳过',
  rest: '休息日',
  due: '可打卡',
  waiting: '倒计时中',
  none: '无记录',
}

/**
 * The same verdict tokens read differently for a bad habit: done means the
 * period was kept clean ("held"), missed means a lapse was recorded. The server
 * computes which token applies; only the wording flips here.
 */
export const BAD_STATUS_LABEL: Record<DayStatus, string> = {
  done: '未破戒',
  partial: '已破戒',
  pending: '保持中',
  missed: '已破戒',
  skipped: '已跳过',
  rest: '休息日',
  due: '可记录',
  waiting: '倒计时中',
  none: '无记录',
}

/** The Chinese label for a verdict, honoring the project's nature. */
export function statusLabel(status: DayStatus, nature: ProjectNature = 'good'): string {
  return nature === 'bad' ? BAD_STATUS_LABEL[status] : STATUS_LABEL[status]
}

/** Statuses that mean the obligation is met (for a bad habit: kept clean). */
export function isComplete(status: DayStatus): boolean {
  return status === 'done'
}

/** Statuses where a tap records a check-in (rather than opening records). */
export function isCheckable(status: DayStatus): boolean {
  return status === 'pending' || status === 'partial' || status === 'due' || status === 'waiting'
}

/**
 * The per-day equivalent of a period target, used to scale heatmap intensity:
 * a weekly goal of 7 counts one per day, a monthly goal of 30 about one per day.
 */
export function dailyEquivalentTarget(p: Project): number {
  switch (p.schedule) {
    case 'weekly':
      return p.target / 7
    case 'monthly': {
      const start = parseDay(p.status?.periodStart ?? p.startDate)
      const days = start === null ? 30 : new Date(start.getFullYear(), start.getMonth() + 1, 0).getDate()
      return p.target / days
    }
    case 'interval_days':
      return p.target / Math.max(1, p.interval)
    case 'interval_hours':
      return Math.max(1, Math.floor(24 / Math.max(1, p.interval)))
    default:
      return Math.max(1, p.target)
  }
}

/**
 * Heatmap intensity 0..4 from a day's value against its daily-equivalent
 * target. One hue, light → dark: the sequential rule. Status markers (skip,
 * miss, rest) are shapes, not colors, so they survive any color vision.
 */
export function heatLevel(cell: HeatCell, dailyTarget: number, nature: ProjectNature = 'good'): number {
  if (nature === 'bad') {
    // Abstention calendar: a clean day fills with the ink, a lapse day stays
    // empty and carries the × marker instead. Magnitude is not the story.
    return cell.status === 'done' ? 4 : 0
  }
  if (cell.value <= 0) return 0
  const ratio = cell.value / Math.max(0.001, dailyTarget)
  if (ratio >= 1) return 4
  if (ratio >= 0.66) return 3
  if (ratio >= 0.33) return 2
  return 1
}

/** Levels for the cross-project activity heatmap, scaled by the busiest day. */
export function activityLevel(count: number, maxCount: number): number {
  if (count <= 0 || maxCount <= 0) return 0
  const ratio = count / maxCount
  if (ratio >= 0.85) return 4
  if (ratio >= 0.55) return 3
  if (ratio >= 0.25) return 2
  return 1
}

/** The Chinese name of an ink token, for swatch labels. */
export const INK_NAMES: Record<string, string> = {
  red: '朱红',
  orange: '橙',
  amber: '琥珀',
  yellow: '金黄',
  lime: '青柠',
  green: '翠绿',
  mint: '薄荷',
  teal: '碧青',
  cyan: '天青',
  blue: '青蓝',
  indigo: '靛蓝',
  violet: '紫藤',
  purple: '紫罗兰',
  pink: '品红',
  rose: '玫瑰',
  slate: '石板',
}

/**
 * What a stamp tap does: projects measured in units other than "次" with a
 * target above 1 open the record sheet (entering 30 minutes one tap at a time
 * would be silly); everything else quick-checks one unit.
 */
export function needsValueSheet(p: Project): boolean {
  return p.schedule !== 'interval_hours' && p.target > 1 && p.unit !== '次'
}
