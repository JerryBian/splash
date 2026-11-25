import { Suspense, useMemo, useState, useTransition } from 'react'
import { api } from '../api/client'
import type { Checkin, Project } from '../api/types'
import { useApp } from '../hooks/useApp'
import { useLoaded } from '../hooks/useLoaded'
import { navigate } from '../hooks/useNow'
import { fetchDay, invalidate } from '../lib/async'
import { addDays, formatClock, formatDayCN, formatMonthShort, parseDay } from '../lib/date'
import { describeError } from '../lib/errors'
import { formatPercent, formatValue, periodLabel, progressLabel, scheduleLabel } from '../lib/format'
import { dailyEquivalentTarget, isBad, statusLabel } from '../lib/schedule'
import { CheckinSheet } from './CheckinSheet'
import { Heatmap, TrendBars } from './charts'
import {
  AlertIcon,
  ArchiveIcon,
  CheckIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  ClockIcon,
  FlameIcon,
  LockIcon,
  PencilIcon,
  PlusIcon,
  SkipIcon,
  TargetIcon,
  TrashIcon,
  TrophyIcon,
} from './icons'
import { ConfirmDialog } from './overlays'
import { Button, EmptyState, IconButton, NatureBadge, RecordBadges, StatTile } from './ui'

/**
 * The project page: identity, the four numbers that matter, the stamp
 * calendar, the trend, and the history. Tapping a heatmap day opens that
 * day's records with skip and backfill controls — the calendar is an input,
 * not just a picture.
 *
 * Data loads with use() + Suspense; mutations bump a version key inside a
 * transition, so the page refetches without flashing a skeleton.
 */
export function ProjectDetail({ projectId }: { projectId: string }) {
  return (
    <Suspense fallback={<DetailLoading />}>
      <DetailBody projectId={projectId} />
    </Suspense>
  )
}

function DetailBody({ projectId }: { projectId: string }) {
  const { projects, categories, today, refresh, toast } = useApp()
  const [version, setVersion] = useState(0)
  const [, startTransition] = useTransition()
  const [sheetDate, setSheetDate] = useState<string | null>(null)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [busy, setBusy] = useState(false)

  const result = useLoaded(`stats:${projectId}:${today}:${version}`, () =>
    api.projectStats(projectId, { weeks: 26, date: today }),
  )

  // Before any early return: hooks must run in the same order every render.
  const barStyle = useMemo(() => {
    const s = result.ok ? result.data.summary : null
    const r = s !== null && s.target > 0 ? Math.min(1, s.value / s.target) : 0
    return { width: `${r * 100}%` }
  }, [result])

  const reload = () => {
    invalidate(`stats:${projectId}:`)
    invalidate(`cal:${projectId}:`)
    invalidate(`day:${projectId}:`)
    startTransition(() => setVersion((v) => v + 1))
  }

  const afterMutation = async () => {
    await refresh()
    reload()
  }

  if (!result.ok) {
    return (
      <div className="view">
        <DetailBack />
        <EmptyState
          icon={<TargetIcon size={30} />}
          title="无法加载项目"
          message={describeError(result.error)}
          action={<Button onClick={reload}>重试</Button>}
        />
      </div>
    )
  }

  const stats = result.data
  const project = stats.project
  const category = categories.find((c) => c.id === project.categoryId)
  const summary = stats.summary
  const isInterval = project.schedule === 'interval_hours'
  const bad = isBad(project)

  const toggleArchive = async () => {
    setBusy(true)
    try {
      await api.updateProject(projectId, { archived: !project.archived }, `"v${project.version}"`)
      toast(project.archived ? '已恢复项目' : '已归档项目', 'ok')
      await afterMutation()
    } catch (caught) {
      toast(describeError(caught), 'error')
    } finally {
      setBusy(false)
    }
  }

  const doDelete = async () => {
    setBusy(true)
    try {
      await api.deleteProject(projectId)
      toast('项目已删除', 'ok')
      await refresh()
      window.history.back()
    } catch (caught) {
      toast(describeError(caught), 'error')
      setBusy(false)
    }
  }

  return (
    <div className="view detail" data-ink={project.color}>
      <DetailBack />

      <header className="detail-hero">
        <span className="detail-icon" aria-hidden="true">
          {project.icon}
        </span>
        <div className="detail-id">
          <h1 className="detail-name">
            {project.name}
            <NatureBadge nature={project.nature} />
          </h1>
          {project.description !== undefined && project.description !== '' ? (
            <p className="detail-desc">{project.description}</p>
          ) : null}
          <div className="detail-chips">
            {category !== undefined ? (
              <span className="mini-chip">
                {category.icon} {category.name}
              </span>
            ) : null}
            <span className="mini-chip">{scheduleLabel(project)}</span>
            <span className="mini-chip">始于 {formatDayCN(project.startDate)}</span>
            {project.archived ? <span className="mini-chip">已归档</span> : null}
          </div>
        </div>
      </header>

      <div className="detail-cols">
      <div className="detail-col-a">
      <section className="detail-now" aria-label="当前进度">
        <div className="detail-now-head">
          <span className="detail-status" data-status={summary.status}>
            {statusLabel(summary.status, project.nature)}
          </span>
          <span className="detail-period">
            {isInterval
              ? summary.dueAt !== undefined && summary.status === 'waiting'
                ? `下次打卡 ${formatClock(summary.dueAt)}`
                : '现在可以打卡'
              : periodLabel(project, summary.periodStart, summary.periodEnd, today)}
          </span>
        </div>
        {!isInterval ? (
          <div className="detail-progress">
            {bad ? null : (
              <span className="bar bar-big">
                <span className="bar-fill" style={barStyle} />
              </span>
            )}
            <span className="detail-progress-text">{progressLabel(project, summary.value, summary.target)}</span>
          </div>
        ) : (
          <p className="detail-progress-text">
            今天已记录 {summary.count} 次
            {summary.lastCheckinAt !== undefined ? ` · 上次 ${formatClock(summary.lastCheckinAt)}` : ''}
          </p>
        )}
        <Button onClick={() => setSheetDate(today)}>
          <PlusIcon size={15} /> {bad ? '记录 / 补卡' : '打卡 / 补卡'}
        </Button>
      </section>

      <HistoryCalendar
        project={project}
        today={today}
        version={version}
        onBackfill={(day) => setSheetDate(day)}
        onChanged={afterMutation}
      />

      <section className="detail-section detail-trend panel chart-panel" aria-label="趋势">
        <h2 className="section-head">
          <TargetIcon size={15} /> 趋势
          <span className="section-hint">虚线为目标</span>
        </h2>
        <TrendBars
          bars={stats.periods}
          color={project.color}
          unit={isInterval ? '次' : project.unit}
          nature={project.nature}
          labelFor={(bar) => {
            if (project.schedule === 'monthly') return formatMonthShort(bar.start)
            const d = parseDay(bar.start)
            if (d === null) return ''
            if (project.schedule === 'weekly') return `${d.getMonth() + 1}/${d.getDate()}`
            return String(d.getDate())
          }}
        />
      </section>

      </div>
      <div className="detail-col-b">
      <section className="stat-grid" aria-label="统计">
        <StatTile label={bad ? '当前坚持' : '当前连续'} value={summary.streak} icon={<FlameIcon size={14} />} sub="个周期" />
        <StatTile label={bad ? '最佳坚持' : '最佳连续'} value={summary.bestStreak} icon={<TrophyIcon size={14} />} sub="个周期" />
        <StatTile
          label={isInterval ? '按时率' : bad ? '守住率' : '完成率'}
          value={formatPercent(isInterval ? stats.totals.onTimeRate : stats.totals.completionRate)}
          icon={<TargetIcon size={14} />}
          sub={
            isInterval
              ? `${stats.totals.lateCount} 次超时`
              : bad
                ? `${stats.totals.done} 守住 / ${stats.totals.missed} 破戒`
                : `${stats.totals.done} 完成 / ${stats.totals.missed} 缺失`
          }
        />
        <StatTile
          label={bad ? '破戒次数' : '总打卡'}
          value={stats.totals.checkins}
          icon={<CheckIcon size={14} />}
          sub={bad ? `${stats.totals.activeDays} 个破戒日 · ${stats.totals.backfilled} 补卡` : `${stats.totals.activeDays} 个活跃日 · ${stats.totals.backfilled} 补卡`}
        />
      </section>

      {project.prerequisites.length > 0 ? (
        <section className="detail-section panel chart-panel" aria-label="前置条件">
          <h2 className="section-head">
            <LockIcon size={15} /> 前置条件
            <span className="section-hint">同日全部满足才解锁</span>
          </h2>
          <ul className="prereq-list">
            {project.prerequisites.map((id) => {
              const prereq = projects.find((p) => p.id === id)
              const unmet = summary.unmet.includes(id)
              return (
                <li key={id} data-unmet={unmet ? 'true' : undefined}>
                  <span className="prereq-name">
                    <span aria-hidden="true">{prereq?.icon ?? '❓'}</span> {prereq?.name ?? id}
                  </span>
                  <span className="prereq-state">{unmet ? '今日未满足' : '今日已满足'}</span>
                </li>
              )
            })}
          </ul>
          <p className="sheet-note">未满足前置时打卡会记一次违规；事后补卡前置可自动消除。</p>
        </section>
      ) : null}

      {stats.totals.violations > 0 ? (
        <section className="detail-section panel chart-panel" aria-label="前置违规">
          <h2 className="section-head">
            <AlertIcon size={15} /> 前置违规
            <span className="section-hint">{stats.totals.violations} 次</span>
          </h2>
          <ul className="record-list">
            {stats.recent
              .filter((r) => r.violated)
              .slice(0, 6)
              .map((r) => (
                <li key={r.id} className="record">
                  <span className="record-value">{formatDayCN(r.localDate)}</span>
                  <span className="record-meta">
                    未满足：{r.unmet.map((id) => projects.find((p) => p.id === id)?.name ?? id).join('、')}
                  </span>
                  <RecordBadges backfilled={r.backfilled} violated={r.violated} />
                </li>
              ))}
          </ul>
        </section>
      ) : null}

      <HistoryList key={version} project={project} initial={stats.recent} onRemove={removeRecent} />

      <footer className="detail-actions">
        <Button variant="soft" onClick={() => navigate(`#edit/${project.id}`)}>
          <PencilIcon size={15} /> 编辑
        </Button>
        <Button variant="ghost" pending={busy} onClick={() => void toggleArchive()}>
          <ArchiveIcon size={15} /> {project.archived ? '恢复' : '归档'}
        </Button>
        <Button variant="danger" onClick={() => setConfirmDelete(true)}>
          <TrashIcon size={15} /> 删除
        </Button>
      </footer>
      </div>
      </div>

      <ConfirmDialog
        open={confirmDelete}
        title={`删除「${project.name}」？`}
        message="该项目的所有打卡记录和跳过标记都会一并删除，无法恢复。建议先归档。"
        confirmLabel="删除"
        danger
        pending={busy}
        onConfirm={() => void doDelete()}
        onClose={() => setConfirmDelete(false)}
      />

      {sheetDate !== null ? (
        <CheckinSheet
          project={project}
          initialDate={sheetDate}
          onClose={() => {
            setSheetDate(null)
            void afterMutation()
          }}
        />
      ) : null}
    </div>
  )

  async function removeRecent(id: string) {
    try {
      await api.deleteCheckin(id)
      toast('已撤销这条记录', 'info')
      await afterMutation()
    } catch (caught) {
      toast(describeError(caught), 'error')
    }
  }
}

/**
 * The stamp calendar with year navigation. The current view is a rolling year;
 * stepping back fetches that calendar year's heatmap (the API computes weeks
 * ending an as-of date), so years of history are reachable, not just the last
 * few months. The year switch suspense is local so the rest of the page never
 * flashes a skeleton. Each day stays tappable for records, skip and backfill.
 */
function HistoryCalendar({
  project,
  today,
  version,
  onBackfill,
  onChanged,
}: {
  project: Project
  today: string
  version: number
  onBackfill: (day: string) => void
  onChanged: () => Promise<void>
}) {
  const currentYear = Number(today.slice(0, 4))
  const startYear = Number(project.startDate.slice(0, 4))
  const [year, setYear] = useState(currentYear)
  const [selectedDay, setSelectedDay] = useState<string | null>(null)

  const step = (next: number) => {
    setYear(next)
    setSelectedDay(null)
  }

  return (
    <section className="detail-section detail-heat panel chart-panel" aria-label="打卡日历">
      <h2 className="section-head">
        <ClockIcon size={15} /> 打卡日历
        <span className="section-hint">点按某天可查看、跳过或补卡</span>
      </h2>
      <div className="calendar-nav">
        <IconButton label="上一年" disabled={year <= startYear} onClick={() => step(year - 1)}>
          <ChevronLeftIcon />
        </IconButton>
        <span className="calendar-year" aria-live="polite">
          {year >= currentYear ? `${currentYear} 年 · 至今` : `${year} 年`}
        </span>
        <IconButton label="下一年" disabled={year >= currentYear} onClick={() => step(year + 1)}>
          <ChevronRightIcon />
        </IconButton>
      </div>
      <Suspense fallback={<div className="calendar-loading"><span className="skeleton skeleton-heat" /></div>}>
        <CalendarBody
          key={year}
          project={project}
          today={today}
          year={year}
          version={version}
          selectedDay={selectedDay}
          onSelect={(day) => setSelectedDay((cur) => (cur === day ? null : day))}
          onBackfill={onBackfill}
          onChanged={onChanged}
        />
      </Suspense>
    </section>
  )
}

function CalendarBody({
  project,
  today,
  year,
  version,
  selectedDay,
  onSelect,
  onBackfill,
  onChanged,
}: {
  project: Project
  today: string
  year: number
  version: number
  selectedDay: string | null
  onSelect: (day: string) => void
  onBackfill: (day: string) => void
  onChanged: () => Promise<void>
}) {
  const currentYear = Number(today.slice(0, 4))
  // A past year is viewed as of its last day; the current year as of today.
  const asOf = year >= currentYear ? today : `${year}-12-31`
  const result = useLoaded(`cal:${project.id}:${year}:${version}`, () =>
    api.projectStats(project.id, { weeks: 53, date: asOf }),
  )
  if (!result.ok) {
    return <p className="sheet-note">{describeError(result.error)}</p>
  }
  const cells = result.data.heatmap
  return (
    <>
      <Heatmap
        cells={cells}
        color={project.color}
        dailyTarget={dailyEquivalentTarget(project)}
        unit={project.unit}
        nature={project.nature}
        selected={selectedDay ?? undefined}
        onSelect={onSelect}
      />
      {selectedDay !== null ? (
        <Suspense fallback={<div className="day-panel"><p className="sheet-note">读取中…</p></div>}>
          <DayPanel
            project={project}
            day={selectedDay}
            version={version}
            verdictLabel={statusLabel(cells.find((c) => c.date === selectedDay)?.status ?? 'none', project.nature)}
            onBackfill={() => onBackfill(selectedDay)}
            onChanged={onChanged}
          />
        </Suspense>
      ) : null}
    </>
  )
}

/**
 * The full record history, paginated backwards by local date. The first page
 * comes from the stats response; "加载更早记录" walks older pages (deduping by
 * id) so a habit with years of check-ins is fully browsable, not just the last
 * thirty. Undo lives on every row.
 */
function HistoryList({
  project,
  initial,
  onRemove,
}: {
  project: Project
  initial: Checkin[]
  onRemove: (id: string) => Promise<void>
}) {
  const [records, setRecords] = useState<Checkin[]>(initial)
  const [cursor, setCursor] = useState<string | null>(
    initial.length > 0 ? initial[initial.length - 1]!.localDate : null,
  )
  const [loading, setLoading] = useState(false)
  const [done, setDone] = useState(initial.length < 30)
  const [expanded, setExpanded] = useState(false)

  const loadMore = async () => {
    if (loading || cursor === null) return
    setLoading(true)
    try {
      const page = await api.listCheckins(project.id, { to: cursor, limit: 50 })
      setRecords((cur) => {
        const seen = new Set(cur.map((r) => r.id))
        return [...cur, ...page.filter((r) => !seen.has(r.id))]
      })
      if (page.length < 50) {
        setDone(true)
      } else {
        setCursor(addDays(page[page.length - 1]!.localDate, -1))
      }
    } catch {
      setDone(true)
    } finally {
      setLoading(false)
    }
  }

  const shown = expanded ? records : records.slice(0, 6)

  return (
    <section className="detail-section detail-history panel chart-panel" aria-label="记录历史">
      <h2 className="section-head">
        <CheckIcon size={15} /> 记录历史
        <span className="section-hint">已加载 {records.length} 条</span>
      </h2>
      {records.length === 0 ? (
        <p className="sheet-note">还没有记录，去盖下第一个印章吧。</p>
      ) : (
        <>
          <ul className="record-list">
            {shown.map((record) => (
              <li key={record.id} className="record">
                <span className="record-value">
                  {formatValue(record.value)} {project.unit}
                </span>
                <span className="record-meta">
                  {formatDayCN(record.localDate)} {formatClock(record.checkedAt)}
                  {record.mood !== undefined && record.mood !== '' ? ` · ${record.mood}` : ''}
                  {record.note !== undefined && record.note !== '' ? ` · ${record.note}` : ''}
                </span>
                <RecordBadges backfilled={record.backfilled} violated={record.violated} />
                <IconButton label="撤销这条记录" onClick={() => void onRemove(record.id)}>
                  <TrashIcon size={15} />
                </IconButton>
              </li>
            ))}
          </ul>
          <div className="history-controls">
            {records.length > 6 ? (
              <button
                type="button"
                className="text-toggle"
                aria-expanded={expanded}
                onClick={() => setExpanded((open) => !open)}
              >
                {expanded ? '收起' : `展开已加载 ${records.length} 条`}
              </button>
            ) : null}
            {!done ? (
              <Button variant="soft" pending={loading} onClick={() => void loadMore()}>
                加载更早记录
              </Button>
            ) : (
              <span className="sheet-note">已加载全部记录</span>
            )}
          </div>
        </>
      )}
    </section>
  )
}

/** One heatmap day: records, skip toggle and backfill, from the shared cache. */
function DayPanel({
  project,
  day,
  version,
  verdictLabel,
  onBackfill,
  onChanged,
}: {
  project: Project
  day: string
  version: number
  verdictLabel: string
  onBackfill: () => void
  onChanged: () => Promise<void>
}) {
  const { toast } = useApp()
  const result = useLoaded(`day:${project.id}:${day}:${version}`, () => fetchDay(project.id, day))
  const [busy, setBusy] = useState(false)

  if (!result.ok) {
    return (
      <div className="day-panel">
        <p className="field-error">{describeError(result.error)}</p>
      </div>
    )
  }
  const { records, skipped } = result.data

  const toggleSkip = async () => {
    setBusy(true)
    try {
      if (skipped) {
        await api.deleteSkip(project.id, day)
        toast('已取消跳过', 'info')
      } else {
        await api.createSkip(project.id, day)
        toast('已跳过这一天，连续记录不受影响', 'ok')
      }
      await onChanged()
    } catch (caught) {
      toast(describeError(caught), 'error')
    } finally {
      setBusy(false)
    }
  }

  const removeRecord = async (id: string) => {
    try {
      await api.deleteCheckin(id)
      toast('已撤销这条记录', 'info')
      await onChanged()
    } catch (caught) {
      toast(describeError(caught), 'error')
    }
  }

  return (
    <div className="day-panel">
      <div className="day-panel-head">
        <strong>{formatDayCN(day)}</strong>
        <span className="day-panel-status">{verdictLabel}</span>
      </div>
      {records.length === 0 ? (
        <p className="sheet-note">这一天没有记录。</p>
      ) : (
        <ul className="record-list">
          {records.map((record) => (
            <li key={record.id} className="record">
              <span className="record-value">
                {formatValue(record.value)} {project.unit}
              </span>
              <span className="record-meta">
                {formatClock(record.checkedAt)}
                {record.mood !== undefined && record.mood !== '' ? ` · ${record.mood}` : ''}
                {record.note !== undefined && record.note !== '' ? ` · ${record.note}` : ''}
              </span>
              <RecordBadges backfilled={record.backfilled} violated={record.violated} />
              <IconButton label="撤销这条记录" onClick={() => void removeRecord(record.id)}>
                <TrashIcon size={15} />
              </IconButton>
            </li>
          ))}
        </ul>
      )}
      <div className="day-panel-actions">
        <Button variant="soft" onClick={onBackfill}>
          <PlusIcon size={14} /> 补卡
        </Button>
        <Button variant="ghost" pending={busy} onClick={() => void toggleSkip()}>
          <SkipIcon size={14} /> {skipped ? '取消跳过' : '跳过这一天'}
        </Button>
      </div>
    </div>
  )
}

function DetailBack() {
  return (
    <div className="detail-back">
      <IconButton label="返回" onClick={() => window.history.back()}>
        <ChevronLeftIcon />
      </IconButton>
    </div>
  )
}

function DetailLoading() {
  return (
    <div className="view">
      <DetailBack />
      <div className="detail-skeleton" aria-label="加载中">
        <span className="skeleton skeleton-hero" />
        <span className="skeleton skeleton-tiles" />
        <span className="skeleton skeleton-heat" />
      </div>
    </div>
  )
}
