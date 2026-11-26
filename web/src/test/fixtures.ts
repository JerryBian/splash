import type { Bootstrap, Overview, Project, ProjectStatus } from '../api/types'

/**
 * Shared test fixtures. The "today" of every fixture is Saturday 2026-11-21,
 * matching the Go tests' epoch so the two suites tell the same story.
 */

export const TEST_TODAY = '2026-11-21'

export function makeStatus(overrides: Partial<ProjectStatus> = {}): ProjectStatus {
  return {
    status: 'pending',
    periodStart: TEST_TODAY,
    periodEnd: TEST_TODAY,
    value: 0,
    target: 1,
    todayValue: 0,
    count: 0,
    streak: 0,
    bestStreak: 0,
    unmet: [],
    ...overrides,
  }
}

export function makeProject(overrides: Partial<Project> = {}): Project {
  return {
    id: 'p-1',
    name: '冥想',
    description: '',
    icon: '🧘',
    color: 'teal',
    categoryId: '',
    nature: 'good',
    schedule: 'daily',
    mode: 'check',
    target: 1,
    unit: '次',
    weekdays: [],
    interval: 0,
    targetTime: '',
    reminderTime: '',
    startDate: '2026-11-01',
    archived: false,
    pinned: false,
    prerequisites: [],
    createdAt: '2026-11-01T00:00:00Z',
    updatedAt: '2026-11-01T00:00:00Z',
    version: 1,
    status: makeStatus(),
    ...overrides,
  }
}

export function makeOverview(overrides: Partial<Overview> = {}): Overview {
  return {
    today: { date: TEST_TODAY, total: 1, done: 0, pending: 1, skipped: 0, due: 0, slipped: 0 },
    totals: { checkins: 0, activeProjects: 1, bestStreak: 0, monthCheckins: 0, slips: 0, monthSlips: 0 },
    activity: Array.from({ length: 7 }, (_, i) => ({
      date: `2026-11-${15 + i}`,
      count: 0,
      value: 0,
      slips: 0,
    })),
    categories: [],
    leaders: [],
    trend: [],
    violations: 0,
    recentViolations: [],
    ...overrides,
  }
}

export function makeBootstrap(overrides: Partial<Bootstrap> = {}): Bootstrap {
  return {
    session: { role: 'admin', username: 'admin' },
    projects: [makeProject()],
    categories: [],
    overview: makeOverview(),
    today: TEST_TODAY,
    ...overrides,
  }
}
