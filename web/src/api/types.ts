/**
 * Wire types for the splash API.
 *
 * These mirror the Go structs in internal/api. They are maintained by hand:
 * the two sides are close enough that a generator would cost more than it
 * saves, but that also means a change to a Go response struct must be made
 * here too. `pnpm test` and the Go tests together catch most drift.
 */

/** The two access roles. Mirrors internal/auth. */
export type Role = 'admin' | 'anonymous'

export type Session = {
  role: Role
  /** Present only for the administrator. */
  username?: string
}

/**
 * The closed ink palette. Stored as token names so light and dark themes can
 * render different steps of the same hue. The order is the validated picker
 * order (adjacent inks stay distinguishable under CVD); every colored mark in
 * the UI also carries an icon or a name, never color alone.
 */
export type InkColor =
  | 'red'
  | 'orange'
  | 'amber'
  | 'yellow'
  | 'lime'
  | 'green'
  | 'mint'
  | 'teal'
  | 'cyan'
  | 'blue'
  | 'indigo'
  | 'violet'
  | 'purple'
  | 'pink'
  | 'rose'
  | 'slate'

export const INK_COLORS: readonly InkColor[] = [
  'red', 'orange', 'amber', 'yellow', 'lime', 'green', 'mint', 'teal',
  'cyan', 'blue', 'indigo', 'violet', 'purple', 'pink', 'rose', 'slate',
]

/** How a project's periods are laid out. Mirrors store.ScheduleType. */
export type ScheduleType =
  | 'daily'
  | 'weekdays'
  | 'weekly'
  | 'monthly'
  | 'interval_days'
  | 'interval_hours'

/** Day/period verdicts. Mirrors the Status* constants in store/schedule.go. */
export type DayStatus =
  | 'done'
  | 'partial'
  | 'pending'
  | 'missed'
  | 'skipped'
  | 'rest'
  | 'due'
  | 'waiting'
  | 'none'

export type Category = {
  id: string
  name: string
  icon: string
  color: InkColor
  createdAt: string
}

/** The project's state right now, computed server-side for the client's day. */
export type ProjectStatus = {
  status: DayStatus
  periodStart: string
  periodEnd: string
  /** Progress in the current period, in the project's unit. */
  value: number
  /** 0 for interval_hours, which has no period target. */
  target: number
  todayValue: number
  /** Check-ins in the current period. */
  count: number
  streak: number
  bestStreak: number
  /** RFC 3339; interval_hours only. */
  dueAt?: string
  lastCheckinAt?: string
  /** Prerequisite project ids not resolved today (gate still closed). */
  unmet: string[]
}

/** How completion is measured, orthogonal to the schedule (period layout). */
export type ProjectMode = 'check' | 'value'

/**
 * Whether a project is a habit to build or a habit to break. Mirrors the
 * Nature* constants in store/domain.go. For a bad habit a check-in records a
 * lapse, the streak counts periods survived clean, and the verdict tokens
 * invert (done = held, missed = lapsed) — see lib/schedule.ts for the labels.
 */
export type ProjectNature = 'good' | 'bad'

export type Project = {
  id: string
  name: string
  description?: string
  icon: string
  color: InkColor
  /** "" when uncategorized. */
  categoryId: string
  /** good = build the habit; bad = break it. Every project needs login to see. */
  nature: ProjectNature
  schedule: ScheduleType
  /** check = one tick completes the period; value = accumulate to target. */
  mode: ProjectMode
  target: number
  unit: string
  /** 1 (Monday) .. 7 (Sunday); only for the weekdays schedule. */
  weekdays: number[]
  /** Hours or days, per schedule. */
  interval: number
  startDate: string
  archived: boolean
  /** Project ids that must be resolved the same day before this one unlocks. */
  prerequisites: string[]
  createdAt: string
  updatedAt: string
  /** Revision counter; sent back as If-Match to detect lost updates. */
  version: number
  /** Present on listings and single reads, absent on write responses. */
  status?: ProjectStatus
}

export type Checkin = {
  id: string
  projectId: string
  value: number
  note?: string
  mood?: string
  /** The client's calendar day, YYYY-MM-DD. */
  localDate: string
  checkedAt: string
  tzOffset: number
  /** Recorded on a later calendar day than the day it credits. */
  backfilled: boolean
  /** Derived: landed while a prerequisite was unmet on localDate. */
  violated: boolean
  /** Derived: prerequisite project ids unmet on localDate. */
  unmet: string[]
  createdAt: string
}

export type Skip = {
  id: string
  projectId: string
  date: string
  reason?: string
  createdAt: string
}

export type HeatCell = {
  date: string
  value: number
  count: number
  status: DayStatus
}

export type PeriodBar = {
  key: string
  start: string
  end: string
  value: number
  target: number
  status: DayStatus
}

export type Totals = {
  checkins: number
  value: number
  activeDays: number
  done: number
  missed: number
  skipped: number
  completionRate: number
  onTimeRate: number
  lateCount: number
  backfilled: number
  violations: number
}

export type ProjectStats = {
  project: Project
  summary: ProjectStatus
  totals: Totals
  heatmap: HeatCell[]
  periods: PeriodBar[]
  recent: Checkin[]
  skips: Skip[]
}

export type Overview = {
  today: { date: string; total: number; done: number; pending: number; skipped: number; due: number; slipped: number }
  totals: {
    checkins: number
    activeProjects: number
    bestStreak: number
    monthCheckins: number
    /** Bad-habit lapses, kept apart from the good-habit check-in totals. */
    slips: number
    monthSlips: number
  }
  /** Last 56 days, oldest first. count/value are good habits; slips are lapses. */
  activity: { date: string; count: number; value: number; slips: number }[]
  categories: { id: string; name: string; icon: string; color: InkColor; count: number }[]
  leaders: { id: string; name: string; icon: string; color: InkColor; nature: ProjectNature; streak: number; bestStreak: number }[]
  trend: { weekStart: string; count: number; slips: number }[]
  violations: number
  recentViolations: Checkin[]
}

export type ProjectInput = {
  name: string
  description?: string
  icon?: string
  color?: InkColor
  categoryId?: string
  nature?: ProjectNature
  schedule: ScheduleType
  mode?: ProjectMode
  target?: number
  unit?: string
  weekdays?: number[]
  interval?: number
  startDate: string
  prerequisites?: string[]
}

/** A partial update; an omitted field means "leave it alone". */
export type ProjectPatch = {
  name?: string
  description?: string
  icon?: string
  color?: InkColor
  /** "" clears the category. */
  categoryId?: string
  nature?: ProjectNature
  schedule?: ScheduleType
  mode?: ProjectMode
  target?: number
  unit?: string
  weekdays?: number[]
  interval?: number
  startDate?: string
  archived?: boolean
  /** Replaces the whole prerequisite list; [] clears it. */
  prerequisites?: string[]
}

export type CheckinInput = {
  value: number
  note?: string
  mood?: string
  /** Empty = today; a past date is a backfill (补卡). */
  date?: string
  tzOffset?: number
}

export type CategoryInput = {
  name: string
  icon?: string
  color?: InkColor
}

/** A full backup, the shape GET /export produces and POST /import accepts. */
export type ExportData = {
  exportedAt: string
  categories: Category[]
  projects: Project[]
  checkins: Checkin[]
  skips: Skip[]
}

/** How many rows an import added (duplicates by id are skipped). */
export type ImportCounts = {
  categories: number
  projects: number
  checkins: number
  skips: number
}

export type ListProjectsParams = {
  date?: string
  archived?: boolean
  category?: string
  q?: string
}

export type Health = {
  status: string
  uptimeSeconds: number
  goroutines: number
  timestamp: string
  version: string
}

export type ReadinessCheck = {
  name: string
  status: 'ok' | 'error'
  durationMs: number
  detail?: string
}

export type Readiness = {
  status: 'ok' | 'unavailable'
  checks: ReadinessCheck[]
}

export type VersionInfo = {
  service: string
  version: string
  commit: string
  date?: string
  goVersion: string
  platform: string
  environment: string
  repository?: string
  uptimeSeconds: number
}

/** One field-level validation problem. */
export type FieldError = {
  field: string
  message: string
}

/** The error envelope every non-2xx response uses. */
export type ApiErrorBody = {
  code: string
  message: string
  details?: FieldError[]
  requestId?: string
}

/** Everything the app needs before it can paint, fetched in parallel. */
export type Bootstrap = {
  session: Session
  projects: Project[]
  categories: Category[]
  overview: Overview
  today: string
}
