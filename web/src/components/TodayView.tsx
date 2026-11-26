import { useMemo, useState, useOptimistic, type MouseEvent } from 'react'
import { api } from '../api/client'
import type { Project } from '../api/types'
import { useApp } from '../hooks/useApp'
import { navigate } from '../hooks/useNow'
import { parseDay, tzOffset, weekdayName, weekdayOf } from '../lib/date'
import { describeError } from '../lib/errors'
import { activityLevel, isBad, needsValueSheet } from '../lib/schedule'
import { CheckinSheet } from './CheckinSheet'
import { ProgressRing } from './charts'
import { ConfirmDialog } from './overlays'
import { CalendarIcon, PlusIcon, SealMark } from './icons'
import { ProjectCard } from './ProjectCard'
import { Button, Chip, EmptyState } from './ui'

/** A time-of-day greeting for the Today header. */
function greeting(): string {
  const hour = new Date().getHours()
  if (hour < 12) return '早上好'
  if (hour < 18) return '下午好'
  return '晚上好'
}

type CheckinAction = { id: string; value: number }

/** Applies a pending check-in to the local list until the server confirms. */
function optimisticProjects(state: Project[], action: CheckinAction): Project[] {
  return state.map((project) => {
    if (project.id !== action.id || project.status === undefined) return project
    const status = project.status
    const value = status.value + action.value
    const todayValue = status.todayValue + action.value
    // A bad habit's record is a lapse: it spoils the period immediately.
    if (isBad(project)) {
      return {
        ...project,
        status: { ...status, value, todayValue, count: status.count + 1, status: 'missed' },
      }
    }
    const complete = status.target > 0 && value >= status.target
    return {
      ...project,
      status: {
        ...status,
        value,
        todayValue,
        count: status.count + 1,
        status: project.schedule === 'interval_hours' ? 'waiting' : complete ? 'done' : 'partial',
      },
    }
  })
}

/**
 * The Today page: the date as a stamp-sheet header, the week at a glance,
 * then projects grouped by category. One tap checks in a good habit; the seal
 * animation and splatter live in ProjectCard. A bad habit opens the record
 * sheet instead, so logging a lapse is deliberate rather than a stray tap.
 */
export function TodayView() {
  const { projects, categories, overview, today, refresh, toast } = useApp()
  const [filter, setFilter] = useState<string>('all')
  const [sheet, setSheet] = useState<{ project: Project; date: string } | null>(null)
  const [gateProject, setGateProject] = useState<Project | null>(null)
  const [optimistic, applyOptimistic] = useOptimistic(projects, optimisticProjects)

  const date = parseDay(today)
  const dayNumber = date?.getDate() ?? 1
  const month = (date?.getMonth() ?? 0) + 1
  const year = date?.getFullYear() ?? 2026

  // A category can disappear while its chip is selected (deleted in another
  // tab or by another session); fall back to "all" instead of an empty page.
  const activeFilter =
    filter === 'all' || categories.some((category) => category.id === filter) ? filter : 'all'
  const visible = useMemo(
    () => optimistic.filter((p) => !p.archived && (activeFilter === 'all' || p.categoryId === activeFilter)),
    [optimistic, activeFilter],
  )

  const groups = useMemo(() => {
    const used = categories.filter((c) => visible.some((p) => p.categoryId === c.id))
    const uncategorized = visible.filter((p) => p.categoryId === '')
    return [
      ...used.map((c) => ({ key: c.id, icon: c.icon, name: c.name, projects: visible.filter((p) => p.categoryId === c.id) })),
      ...(uncategorized.length > 0 ? [{ key: '', icon: '🗂️', name: '未分类', projects: uncategorized }] : []),
    ]
  }, [categories, visible])

  const quickCheck = (project: Project) => {
    // Quick check-in: optimistic seal now, server truth right after.
    // The splatter fires from ProjectCard's transition effect.
    applyOptimistic({ id: project.id, value: 1 })
    void (async () => {
      try {
        const created = await api.createCheckin(project.id, { value: 1, tzOffset: tzOffset() })
        if (created.violated) toast(`已打卡「${project.name}」，但记了一次前置违规`, 'error')
        await refresh()
      } catch (error) {
        toast(describeError(error), 'error')
        await refresh()
      }
    })()
  }

  const handleStamp = (project: Project, _event: MouseEvent<HTMLButtonElement>) => {
    // Recording a lapse is deliberate: always open the sheet for a bad habit.
    if (isBad(project)) {
      setSheet({ project, date: today })
      return
    }
    const status = project.status?.status
    if (status === 'done' || status === 'skipped' || status === 'rest' || needsValueSheet(project)) {
      setSheet({ project, date: today })
      return
    }
    // Gate closed today: acknowledge the would-be violation before checking.
    if ((project.status?.unmet.length ?? 0) > 0) {
      setGateProject(project)
      return
    }
    quickCheck(project)
  }

  const lastWeek = overview.activity.slice(-7)
  const maxCount = Math.max(1, ...lastWeek.map((d) => d.count))

  return (
    <div className="view">
      <header className="today-head">
        <div className="today-date">
          <span className="today-number">{dayNumber}</span>
          <span className="today-meta">
            <span className="today-month">{month}月</span>
            <span className="today-weekday">
              {weekdayName(weekdayOf(today))} · {year}
            </span>
          </span>
        </div>
        <div className="week-strip" aria-label="最近 7 天活动">
          {lastWeek.map((day) => {
            const d = parseDay(day.date)
            const level = activityLevel(day.count, maxCount)
            return (
              <span key={day.date} className="week-day" data-today={day.date === today ? 'true' : undefined}>
                <span className="week-name">{weekdayName(weekdayOf(day.date))}</span>
                <span className="week-dot" data-level={level} />
                <span className="week-num">{d?.getDate() ?? ''}</span>
              </span>
            )
          })}
        </div>
        <span className="today-ring">
          <ProgressRing value={overview.today.done} max={Math.max(1, overview.today.total)} size={68} stroke={6}>
            <span className="ring-fraction">
              {overview.today.done}
              <span className="ring-total">/{overview.today.total}</span>
            </span>
          </ProgressRing>
        </span>
      </header>

      <p className="today-greeting">
        <SealMark size={14} className="greeting-seal" />
        <span>
          {greeting()}，把每一天盖成印章。
          {overview.today.slipped > 0 ? ` 今天有 ${overview.today.slipped} 个坏习惯破戒了，明天继续守住。` : ''}
        </span>
      </p>

      {categories.length > 0 ? (
        <div className="chip-row" role="group" aria-label="按分类筛选">
          <Chip active={activeFilter === 'all'} onClick={() => setFilter('all')}>
            全部
          </Chip>
          {categories.map((c) => (
            <Chip key={c.id} active={activeFilter === c.id} onClick={() => setFilter(c.id)}>
              <span aria-hidden="true">{c.icon}</span> {c.name}
            </Chip>
          ))}
        </div>
      ) : null}

      <div className="today-actions">
        <Button variant="soft" onClick={() => navigate('#edit/new')}>
          <PlusIcon size={15} /> 新建项目
        </Button>
      </div>

      {visible.length === 0 ? (
        <EmptyState
          icon={<CalendarIcon size={30} />}
          title="今天还没有打卡项目"
          message="创建一个项目，开始盖下第一个印章。"
          action={
            <Button onClick={() => navigate('#edit/new')}>
              <PlusIcon size={16} /> 新建项目
            </Button>
          }
        />
      ) : (
        groups.map((group) => (
          <section key={group.key || 'uncategorized'} className="group" aria-label={group.name}>
            <h2 className="group-head">
              <span aria-hidden="true">{group.icon}</span> {group.name}
              <span className="group-count">
                {group.projects.filter((p) => p.status?.status === 'done').length}/{group.projects.length}
              </span>
            </h2>
            <div className="group-cards">
              {group.projects.map((project) => (
                <ProjectCard
                  key={project.id}
                  project={project}
                  onOpen={() => navigate(`#project/${project.id}`)}
                  onStamp={handleStamp}
                />
              ))}
            </div>
          </section>
        ))
      )}

      {visible.length > 0 ? (
        <button type="button" className="fab" aria-label="新建项目" onClick={() => navigate('#edit/new')}>
          <PlusIcon size={22} strokeWidth={2.2} />
        </button>
      ) : null}

      {sheet !== null ? (
        <CheckinSheet
          project={optimistic.find((p) => p.id === sheet.project.id) ?? sheet.project}
          initialDate={sheet.date}
          onClose={() => setSheet(null)}
        />
      ) : null}

      <ConfirmDialog
        open={gateProject !== null}
        title="前置条件未满足"
        message={
          gateProject === null
            ? ''
            : `「${gateProject.name}」今天还有 ${gateProject.status?.unmet.length ?? 0} 个前置没完成。现在打卡会记一次前置违规；之后补卡前置可自动消除。仍要打卡吗？`
        }
        confirmLabel="仍要打卡"
        onConfirm={() => {
          if (gateProject !== null) quickCheck(gateProject)
          setGateProject(null)
        }}
        onClose={() => setGateProject(null)}
      />
    </div>
  )
}
