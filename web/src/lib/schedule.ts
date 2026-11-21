/**
 * Client-side reading of the server's schedule verdicts: labels, heat levels
 * and the rules that decide what a tap does. The server computes every status;
 * nothing here re-derives streaks or periods, so the two ends cannot disagree.
 */

import type { DayStatus, HeatCell, Project } from '../api/types'
import { parseDay } from './date'

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

/** Statuses that mean the obligation is met. */
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
export function heatLevel(cell: HeatCell, dailyTarget: number): number {
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
  blue: '青蓝',
  green: '翠绿',
  purple: '紫罗兰',
  pink: '品红',
  orange: '橙',
  teal: '碧青',
  yellow: '金黄',
}

/**
 * What a stamp tap does: projects measured in units other than "次" with a
 * target above 1 open the record sheet (entering 30 minutes one tap at a time
 * would be silly); everything else quick-checks one unit.
 */
export function needsValueSheet(p: Project): boolean {
  return p.schedule !== 'interval_hours' && p.target > 1 && p.unit !== '次'
}
