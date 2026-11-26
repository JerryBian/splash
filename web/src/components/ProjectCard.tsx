import { useEffect, useMemo, useRef, useState, type MouseEvent } from 'react'
import type { Project } from '../api/types'
import { useNow } from '../hooks/useNow'
import { inkColorOf, splatter } from '../lib/confetti'
import { formatCountdown } from '../lib/date'
import { progressLabel, scheduleLabel } from '../lib/format'
import { isBad, isComplete } from '../lib/schedule'
import { CheckIcon, LinkIcon, LockIcon, PinIcon, PlusIcon, XIcon } from './icons'
import { NatureBadge } from './ui'

/**
 * One project on the Today page. The stamp button is the app's signature
 * interaction: an empty circle waits, a tap presses a rotated ink seal onto
 * the card and fires a splatter in the project's color. Everything else on
 * the card stays quiet so that one moment carries the personality.
 *
 * A bad habit inverts the moment: the seal marks a lapse (×), a clean card
 * shows 守 ("held"), and tapping opens the record sheet rather than celebrating.
 */
export function ProjectCard({
  project,
  onOpen,
  onStamp,
}: {
  project: Project
  onOpen: () => void
  onStamp: (project: Project, event: MouseEvent<HTMLButtonElement>) => void
}) {
  const status = project.status
  const cardRef = useRef<HTMLDivElement>(null)
  const stampRef = useRef<HTMLButtonElement>(null)
  const prevStatus = useRef(status?.status)
  const [stamping, setStamping] = useState(false)

  const bad = isBad(project)
  const isInterval = project.schedule === 'interval_hours'
  const waiting = isInterval && status?.status === 'waiting'
  const now = useNow(waiting)

  // Fire the seal animation only on the transition into done — not on every
  // re-render of an already-done card. For a bad habit done means "held", so
  // the splatter celebrates a recovered clean day, never a lapse.
  useEffect(() => {
    const current = status?.status
    const previous = prevStatus.current
    prevStatus.current = current
    if (current === 'done' && previous !== undefined && previous !== 'done') {
      setStamping(true)
      const rect = stampRef.current?.getBoundingClientRect()
      if (rect !== undefined) {
        splatter(rect.left + rect.width / 2, rect.top + rect.height / 2, inkColorOf(cardRef.current))
      }
      const timer = window.setTimeout(() => setStamping(false), 700)
      return () => window.clearTimeout(timer)
    }
  }, [status?.status])

  const done = status !== undefined && isComplete(status.status)
  const lapsed = bad && status?.status === 'missed'
  const ratio =
    status === undefined || status.target <= 0
      ? 0
      : Math.max(0, Math.min(1, status.value / status.target))
  const barStyle = useMemo(() => ({ width: `${ratio * 100}%` }), [ratio])

  const stampLabel = bad
    ? lapsed
      ? `${project.name} 已破戒，查看记录`
      : `记录 ${project.name} 破戒`
    : done
      ? `${project.name} 已完成，查看记录`
      : `为 ${project.name} 打卡`

  return (
    <div className="card" data-ink={project.color} data-status={status?.status} data-nature={project.nature} ref={cardRef}>
      <button type="button" className="card-open" onClick={onOpen}>
        <span className="card-icon" aria-hidden="true">
          {project.icon}
        </span>
        <span className="card-main">
          <span className="card-title">
            {project.name}
            <NatureBadge nature={project.nature} />
            {project.pinned ? (
              <span className="card-pin" title="已置顶">
                <PinIcon size={11} />
              </span>
            ) : null}
            {project.archived ? <span className="mini-chip">已归档</span> : null}
          </span>
          <span className="card-sub">
            {scheduleLabel(project)}
            {status !== undefined && status.streak > 0 ? (
              <span className="card-streak" title={bad ? `坚持 ${status.streak} 个周期` : `连续 ${status.streak} 个周期`}>
                🔥{status.streak}
              </span>
            ) : null}
          </span>
          {status !== undefined && status.unmet.length > 0 ? (
            <span className="card-gate" title="前置项目今天还没完成">
              <LockIcon size={11} /> 需先完成 {status.unmet.length} 个前置
            </span>
          ) : null}
          {status !== undefined && status.pendingPosts.length > 0 ? (
            <span className="card-link" data-kind="pending" title="完成后当天还需兑现这些后置项目">
              <LinkIcon size={11} /> 待兑现 {status.pendingPosts.length} 个后置
            </span>
          ) : null}
          {status !== undefined && status.brokenPosts.length > 0 ? (
            <span className="card-link" data-kind="broken" title="后置项目没有在同一天完成">
              <LinkIcon size={11} /> 未兑现 {status.brokenPosts.length} 个后置
            </span>
          ) : null}
          {isInterval ? (
            <span className="card-progress-text">
              {status?.status === 'due'
                ? '现在可以打卡'
                : status?.dueAt !== undefined
                  ? `下次 ${formatCountdown(status.dueAt, now)}`
                  : ''}
              {status !== undefined && status.todayValue > 0 ? ` · 今天 ${status.count} 次` : ''}
            </span>
          ) : (
            <span className="card-progress">
              {bad ? null : (
                <span className="bar">
                  <span className="bar-fill" style={barStyle} />
                </span>
              )}
              <span className="card-progress-text">
                {status === undefined ? '' : progressLabel(project, status.value, status.target)}
              </span>
            </span>
          )}
        </span>
      </button>

      <button
        type="button"
        className="stamp"
        ref={stampRef}
        data-stamping={stamping ? 'true' : undefined}
        data-done={done && !bad ? 'true' : undefined}
        data-lapsed={lapsed ? 'true' : undefined}
        aria-label={stampLabel}
        onClick={(event) => onStamp(project, event)}
      >
        {lapsed ? (
          <span className="seal" data-lapsed="true">
            <XIcon size={18} strokeWidth={2.8} />
          </span>
        ) : bad ? (
          status?.status === 'skipped' || status?.status === 'rest' ? (
            <span className="stamp-rest">休</span>
          ) : (
            <span className="stamp-held">守</span>
          )
        ) : done ? (
          <span className="seal">
            <CheckIcon size={18} strokeWidth={2.8} />
          </span>
        ) : status?.status === 'skipped' || status?.status === 'rest' ? (
          <span className="stamp-rest">休</span>
        ) : waiting && status?.dueAt !== undefined ? (
          <span className="stamp-count">{formatCountdown(status.dueAt, now).slice(0, 5)}</span>
        ) : (
          <PlusIcon size={18} strokeWidth={2.2} className="stamp-plus" />
        )}
      </button>
    </div>
  )
}
