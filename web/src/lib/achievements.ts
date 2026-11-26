import type { Project, ProjectStatus, Totals } from '../api/types'

/**
 * Derived milestones, the gamification layer habit apps use to reward
 * consistency. Nothing is stored: achievements are recomputed from the same
 * streak / total figures the stats already derive, so they can never drift.
 */
export type Achievement = {
  id: string
  label: string
  icon: string
  earned: boolean
  /** 0..1 progress toward the milestone, for the unearned hint. */
  progress: number
}

const STREAK_MILESTONES = [7, 30, 100, 365]
const COUNT_MILESTONES = [10, 50, 100, 500]
const PERIOD_MILESTONES = [10, 50, 100]
const ONTIME_MILESTONES = [10, 30, 100]

/**
 * Builds the badge shelf for one project.
 *
 * The families are deliberately different because the underlying numbers mean
 * different things: a bad habit never earns a "打卡" badge (its check-ins are
 * lapses), so it is rewarded for periods kept clean instead; a time-mode habit
 * with a deadline earns punctuality badges, which a plain check habit has no
 * way to measure. Reusing a count milestone across nature is exactly the kind
 * of design that would make a lapse look like progress.
 */
export function achievements(project: Project, totals: Totals, summary: ProjectStatus): Achievement[] {
  const out: Achievement[] = []
  const bad = project.nature === 'bad'
  const streakWord = bad ? '坚持' : '连续'
  for (const m of STREAK_MILESTONES) {
    out.push({
      id: `streak-${m}`,
      label: `${streakWord} ${m} 周期`,
      icon: m >= 100 ? '🏆' : m >= 30 ? '🥇' : '🔥',
      earned: summary.bestStreak >= m,
      progress: Math.min(1, summary.bestStreak / m),
    })
  }

  const periodWord = bad ? '守住' : '完成'
  for (const m of PERIOD_MILESTONES) {
    out.push({
      id: `period-${m}`,
      label: `${periodWord} ${m} 个周期`,
      icon: m >= 100 ? '🌳' : m >= 50 ? '🌿' : '🌱',
      earned: totals.done >= m,
      progress: Math.min(1, totals.done / m),
    })
  }

  // A good habit's check-ins are progress; a bad habit's are lapses, so its
  // equivalent milestone is already covered by the clean-period family above.
  if (!bad) {
    for (const m of COUNT_MILESTONES) {
      out.push({
        id: `count-${m}`,
        label: `打卡 ${m} 次`,
        icon: m >= 100 ? '💎' : '⭐',
        earned: totals.checkins >= m,
        progress: Math.min(1, totals.checkins / m),
      })
    }
  }

  // Punctuality only exists when there is a deadline (time mode) or a rolling
  // interval that can be missed.
  const timed = project.mode === 'time' ? project.targetTime !== '' : project.schedule === 'interval_hours'
  if (timed) {
    const word = project.mode === 'time' ? '准时' : '按时'
    for (const m of ONTIME_MILESTONES) {
      out.push({
        id: `ontime-${m}`,
        label: `${word} ${m} 次`,
        icon: m >= 100 ? '🏅' : m >= 30 ? '🎯' : '⏰',
        earned: totals.onTimeCount >= m,
        progress: Math.min(1, totals.onTimeCount / m),
      })
    }
  }
  return out
}
