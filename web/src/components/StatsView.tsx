import { useMemo } from 'react'
import { useApp } from '../hooks/useApp'
import { navigate } from '../hooks/useNow'
import { addDays, formatDayCN, parseDay } from '../lib/date'
import { formatValue } from '../lib/format'
import { Donut, Heatmap, ProgressRing, Sparkline, TrendBars } from './charts'
import { AlertIcon, ChartIcon, CheckIcon, FlameIcon, LayersIcon, SealMark, TargetIcon, TrophyIcon } from './icons'
import { EmptyState, NatureBadge, RecordBadges, StatTile } from './ui'
import type { HeatCell } from '../api/types'

/**
 * The dashboard: a hero panel with the day's ring and a 7-day sparkline,
 * four stat tiles (two carrying their own sparklines), then paired chart
 * panels — activity heatmap beside the weekly trend, category donut beside
 * the streak leaderboard. Cross-project charts wear the calm brand celadon;
 * only the category donut uses entity inks, each with a labelled legend.
 */
export function StatsView() {
  const { overview, today, projects } = useApp()

  // Memoized so chart props keep stable identities across renders, and all
  // hooks stay above the early return below.
  const activityCells = useMemo<HeatCell[]>(
    () =>
      overview.activity.map((d) => ({
        date: d.date,
        value: d.count,
        count: d.count,
        status: d.count > 0 ? 'done' : d.date === today ? 'pending' : 'none',
      })),
    [overview.activity, today],
  )
  const last14 = useMemo(() => overview.activity.slice(-14).map((d) => d.count), [overview.activity])
  const last7 = useMemo(() => overview.activity.slice(-7), [overview.activity])
  const last7Counts = useMemo(() => last7.map((d) => d.count), [last7])
  const weekTotal = useMemo(() => last7.reduce((sum, d) => sum + d.count, 0), [last7])
  // Month-to-date series for the "本月打卡" tile: the 14-day series under
  // "总打卡" is about recent activity, not the month.
  const monthSpark = useMemo(
    () =>
      overview.activity
        .filter((day) => day.date.slice(0, 7) === today.slice(0, 7))
        .map((day) => day.count),
    [overview.activity, today],
  )
  const slices = useMemo(
    () =>
      overview.categories.map((c) => ({
        key: c.id || 'none',
        label: c.name,
        icon: c.icon,
        color: c.color,
        value: c.count,
      })),
    [overview.categories],
  )
  const catTotal = useMemo(() => slices.reduce((sum, s) => sum + s.value, 0), [slices])
  const trendBars = useMemo(
    () =>
      overview.trend.map((week) => ({
        key: week.weekStart,
        start: week.weekStart,
        // The API names the week by its Monday; the tooltip wants the range.
        end: addDays(week.weekStart, 6),
        value: week.count,
        target: 0,
        status: week.count > 0 ? ('done' as const) : ('none' as const),
      })),
    [overview.trend],
  )
  const maxCount = Math.max(1, ...overview.activity.map((d) => d.count))
  const pct = Math.round((overview.today.done / Math.max(1, overview.today.total)) * 100)
  const heatStats = useMemo(() => {
    const days = overview.activity
    const total = days.reduce((sum, day) => sum + day.count, 0)
    const best = days.reduce((top, day) => Math.max(top, day.count), 0)
    return {
      activeDays: days.filter((day) => day.count > 0).length,
      average: days.length === 0 ? 0 : total / days.length,
      best,
    }
  }, [overview.activity])

  if (overview.totals.activeProjects === 0) {
    return (
      <div className="view">
        <h1 className="view-title">统计</h1>
        <EmptyState
          icon={<ChartIcon size={30} />}
          title="还没有可统计的项目"
          message="创建项目并开始打卡后，这里会出现趋势和热力图。"
        />
      </div>
    )
  }

  return (
    <div className="view stats">
      <h1 className="view-title">统计</h1>

      <section className="stats-hero" aria-label="今日完成">
        <span className="hero-mark" aria-hidden="true">
          <SealMark size={190} />
        </span>
        <ProgressRing value={overview.today.done} max={Math.max(1, overview.today.total)} size={116} stroke={9}>
          <span className="ring-big">{pct}%</span>
        </ProgressRing>
        <div className="stats-hero-text">
          <strong>
            今日完成 {overview.today.done}/{overview.today.total}
          </strong>
          <span>
            {overview.today.pending > 0 ? `${overview.today.pending} 个待打卡` : '全部完成，漂亮'}
            {overview.today.skipped > 0 ? ` · ${overview.today.skipped} 个跳过` : ''}
            {overview.today.due > 0 ? ` · ${overview.today.due} 个到点` : ''}
            {overview.today.slipped > 0 ? ` · ${overview.today.slipped} 个破戒` : ''}
          </span>
          <span className="stats-hero-date">{formatDayCN(today)}</span>
        </div>
        <div className="hero-spark">
          <Sparkline values={last7Counts} width={132} height={42} label="近 7 天打卡次数" />
          <span>近 7 天 · {weekTotal} 次</span>
        </div>
      </section>

      <section className="stat-grid" aria-label="总览">
        <StatTile label="总打卡" value={overview.totals.checkins} icon={<CheckIcon size={14} />} sub="好习惯 · 次" spark={last14} />
        <StatTile label="本月打卡" value={overview.totals.monthCheckins} icon={<FlameIcon size={14} />} sub="好习惯 · 次" spark={monthSpark} />
        <StatTile label="最佳连续" value={overview.totals.bestStreak} icon={<TrophyIcon size={14} />} sub="个周期" />
        <StatTile label="活跃项目" value={overview.totals.activeProjects} icon={<LayersIcon size={14} />} sub="个" />
        <StatTile label="破戒次数" value={overview.totals.slips} icon={<AlertIcon size={14} />} sub="坏习惯 · 次" />
        <StatTile label="本月破戒" value={overview.totals.monthSlips} icon={<AlertIcon size={14} />} sub="坏习惯 · 次" />
      </section>

      <div className="stats-cols">
        <section className="panel chart-panel" aria-label="活动热力图">
          <h2 className="section-head">
            <ChartIcon size={15} /> 全部活动
            <span className="section-hint">最近 8 周 · 每天好习惯打卡</span>
          </h2>
          <div className="heat-layout">
            <Heatmap cells={activityCells} color="brand" dailyTarget={maxCount} unit=" 次" />
            <aside className="heat-aside" aria-label="活动摘要">
              <span className="heat-fact">
                <strong>{heatStats.activeDays}</strong>
                <small>活跃天数</small>
              </span>
              <span className="heat-fact">
                <strong>{heatStats.average.toFixed(1)}</strong>
                <small>日均次数</small>
              </span>
              <span className="heat-fact">
                <strong>{heatStats.best}</strong>
                <small>单日最多</small>
              </span>
            </aside>
          </div>
        </section>

        <section className="panel chart-panel" aria-label="每周趋势">
          <h2 className="section-head">
            <TargetIcon size={15} /> 每周趋势
            <span className="section-hint">最近 8 周好习惯打卡</span>
          </h2>
          <TrendBars
            bars={trendBars}
            color="brand"
            unit="次"
            labelFor={(bar) => {
              const d = parseDay(bar.start)
              return d === null ? '' : `${d.getMonth() + 1}/${d.getDate()}`
            }}
          />
        </section>
      </div>

      <div className="stats-cols">
        {slices.length > 0 ? (
          <section className="panel chart-panel" aria-label="分类分布">
            <h2 className="section-head">
              <LayersIcon size={15} /> 分类分布
              <span className="section-hint">最近 30 天 · 好习惯</span>
            </h2>
            <div className="donut-row">
              <Donut slices={slices} centerLabel="次" />
              <ul className="donut-legend">
                {slices.map((s) => (
                  <li key={s.key} data-ink={s.color}>
                    <span className="donut-dot" aria-hidden="true" />
                    <span className="donut-name">
                      <span aria-hidden="true">{s.icon}</span> {s.label}
                    </span>
                    <span className="donut-val">{s.value}</span>
                    <span className="donut-pct">{catTotal > 0 ? Math.round((s.value / catTotal) * 100) : 0}%</span>
                  </li>
                ))}
              </ul>
            </div>
          </section>
        ) : null}

        {overview.leaders.length > 0 ? (
          <section className="panel chart-panel" aria-label="连续排行">
            <h2 className="section-head">
              <FlameIcon size={15} /> 连续排行
            </h2>
            <ul className="leaders">
              {overview.leaders.map((leader, index) => (
                <li key={leader.id}>
                  <button
                    type="button"
                    className="leader"
                    data-ink={leader.color}
                    onClick={() => navigate(`#project/${leader.id}`)}
                  >
                    <span className="leader-rank" data-top={index < 3 ? 'true' : undefined}>
                      {index + 1}
                    </span>
                    <span className="leader-icon" aria-hidden="true">
                      {leader.icon}
                    </span>
                    <span className="leader-name">
                      {leader.name}
                      <NatureBadge nature={leader.nature} />
                    </span>
                    <span className="leader-streak" title={leader.nature === 'bad' ? `坚持 ${leader.streak} 个周期` : `连续 ${leader.streak} 个周期`}>
                      🔥 {leader.streak}
                      <span className="leader-best">最佳 {leader.bestStreak}</span>
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          </section>
        ) : null}
      </div>

      {overview.violations > 0 ? (
        <section className="detail-section panel chart-panel violations-panel" aria-label="前置违规">
          <h2 className="section-head">
            <AlertIcon size={15} /> 前置违规
            <span className="section-hint">{overview.violations} 次 · 没完成前置就打了卡</span>
          </h2>
          <ul className="record-list">
            {overview.recentViolations.map((c) => {
              const p = projects.find((x) => x.id === c.projectId)
              return (
                <li key={c.id} className="record">
                  <span className="record-value">
                    <span aria-hidden="true">{p?.icon}</span> {p?.name ?? c.projectId}
                  </span>
                  <span className="record-meta">
                    {formatDayCN(c.localDate)} · 未满足：
                    {c.unmet.map((id) => projects.find((x) => x.id === id)?.name ?? id).join('、')}
                  </span>
                  <RecordBadges backfilled={c.backfilled} violated={c.violated} />
                </li>
              )
            })}
          </ul>
          <p className="sheet-note">补卡对应前置后，该违规会自动消除。</p>
        </section>
      ) : null}

      <p className="stats-foot">数值为 {formatValue(overview.totals.checkins)} 次打卡的累计 · 统计随每次打卡实时更新</p>
    </div>
  )
}
