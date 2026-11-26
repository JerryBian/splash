import type { Project, ProjectStatus, Totals } from '../api/types'
import { achievements } from '../lib/achievements'
import { TrophyIcon } from './icons'

/**
 * The milestone badge row: earned achievements glow in the brand accent,
 * unearned ones recede with a progress hint. Pure derivation, no storage.
 */
export function Achievements({
  project,
  totals,
  summary,
}: {
  project: Project
  totals: Totals
  summary: ProjectStatus
}) {
  const list = achievements(project, totals, summary)
  const earned = list.filter((a) => a.earned).length
  return (
    <section className="detail-section panel chart-panel" aria-label="成就">
      <h2 className="section-head">
        <TrophyIcon size={15} /> 成就
        <span className="section-hint">
          {earned}/{list.length} 已达成
        </span>
      </h2>
      <div className="achievement-row">
        {list.map((a) => (
          <span
            key={a.id}
            className="achievement"
            data-earned={a.earned ? 'true' : undefined}
            title={a.earned ? `已达成：${a.label}` : `${a.label} · ${Math.round(a.progress * 100)}%`}
          >
            <span aria-hidden="true">{a.icon}</span>
            {a.label}
          </span>
        ))}
      </div>
    </section>
  )
}
