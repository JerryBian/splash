import { useEffect, useMemo, useRef, type ReactNode } from 'react'
import type { HeatCell, InkColor, PeriodBar } from '../api/types'

/** Chart hue: a project ink, or the calm brand celadon for cross-project views. */
export type ChartColor = InkColor | 'brand'
import { addDays, formatDayCN, formatMonthShort, formatRangeCN, weekStart, weekdayOf } from '../lib/date'
import { formatValue } from '../lib/format'
import { heatLevel, STATUS_LABEL } from '../lib/schedule'

/**
 * Charts are hand-built SVG/div marks: three small forms, no chart library.
 * The rules they follow: one hue per sequential ramp (light → dark), text in
 * ink tokens never in series color, status encoded by shape as well as color,
 * a tooltip on every mark, and recessive axes.
 */

/** A progress ring drawn as an ink circle. The center carries the number. */
export function ProgressRing({
  value,
  max,
  size = 64,
  stroke = 6,
  children,
}: {
  value: number
  max: number
  size?: number
  stroke?: number
  children?: ReactNode
}) {
  const radius = (size - stroke) / 2
  const circumference = 2 * Math.PI * radius
  const ratio = max <= 0 ? 0 : Math.max(0, Math.min(1, value / max))
  const wrapStyle = useMemo(() => ({ width: size, height: size }), [size])
  return (
    <div className="ring" style={wrapStyle}>
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} aria-hidden="true">
        <circle cx={size / 2} cy={size / 2} r={radius} fill="none" stroke="var(--line)" strokeWidth={stroke} />
        <circle
          className="ring-arc"
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          stroke="var(--ink, var(--brand))"
          strokeWidth={stroke}
          strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={circumference * (1 - ratio)}
          transform={`rotate(-90 ${size / 2} ${size / 2})`}
        />
      </svg>
      <span className="ring-center">{children}</span>
    </div>
  )
}

type Slot = HeatCell & { empty?: boolean }

type WeekColumn = {
  week: string
  monthLabel: string
  cells: Slot[]
}

/**
 * The calendar heatmap: weeks as columns, Monday on top. Intensity is one
 * hue at four steps; skipped days are dashed, missed days carry a small ×,
 * rest days recede — shapes that survive any color vision. Today keeps an
 * outline so "where am I" never depends on color either.
 */
export function Heatmap({
  cells,
  color,
  dailyTarget,
  unit,
  selected,
  onSelect,
}: {
  cells: HeatCell[]
  color: ChartColor
  dailyTarget: number
  unit?: string
  selected?: string
  onSelect?: (date: string) => void
}) {
  const scrollRef = useRef<HTMLDivElement>(null)

  const columns = useMemo<WeekColumn[]>(() => {
    const byWeek = new Map<string, WeekColumn>()
    for (const cell of cells) {
      const week = weekStart(cell.date)
      let column = byWeek.get(week)
      if (column === undefined) {
        // Empty slots carry synthetic dates so every key is data-dependent.
        column = {
          week,
          monthLabel: '',
          cells: Array.from({ length: 7 }, (_, slot) => ({
            date: `${week}#${slot}`,
            value: 0,
            count: 0,
            status: 'none' as const,
            empty: true,
          })),
        }
        byWeek.set(week, column)
      }
      column.cells[weekdayOf(cell.date) - 1] = cell
    }
    const ordered = [...byWeek.values()].toSorted((a, b) => a.week.localeCompare(b.week))
    // Label by the month the week ENDS in (a week starting Aug 31 belongs to
    // September), and keep at least three columns between labels so the text
    // never overlaps its neighbour on a 14px grid.
    let lastMonth = ''
    let lastLabelAt = -3
    ordered.forEach((column, index) => {
      const month = addDays(column.week, 6).slice(0, 7)
      if (month !== lastMonth && index - lastLabelAt >= 3) {
        column.monthLabel = formatMonthShort(addDays(column.week, 6))
        lastMonth = month
        lastLabelAt = index
      }
    })
    return ordered
  }, [cells])

  // Anchor the scroll at today (the right edge) on first paint.
  useEffect(() => {
    const el = scrollRef.current
    if (el !== null && columns.length > 0) el.scrollLeft = el.scrollWidth
  }, [columns])

  const cellLabel = (cell: HeatCell): string => {
    const amount = cell.value > 0 ? `，${formatValue(cell.value)}${unit ?? ''}` : ''
    return `${formatDayCN(cell.date)}：${STATUS_LABEL[cell.status]}${amount}`
  }

  return (
    <div className="heatmap" data-ink={color}>
      <div className="heatmap-months" aria-hidden="true">
        {columns.map((column) => (
          <span key={column.week} className="heatmap-month">
            {column.monthLabel}
          </span>
        ))}
      </div>
      <div className="heatmap-body">
        <div className="heatmap-weekdays" aria-hidden="true">
          <span>一</span>
          <span />
          <span>三</span>
          <span />
          <span>五</span>
          <span />
          <span>日</span>
        </div>
        <div className="heatmap-scroll" ref={scrollRef}>
          <div className="heatmap-grid">
            {columns.map((column) => (
              <div key={column.week} className="heatmap-col">
                {column.cells.map((cell) => {
                  if (cell.empty === true) {
                    return <span key={cell.date} className="heat-cell" data-empty="true" />
                  }
                  const level = heatLevel(cell, dailyTarget)
                  const interactive = onSelect !== undefined
                  const props = {
                    className: 'heat-cell',
                    'data-level': level,
                    'data-status': cell.status,
                    'data-selected': cell.date === selected ? 'true' : undefined,
                    title: cellLabel(cell),
                  }
                  return interactive ? (
                    <button
                      key={cell.date}
                      type="button"
                      {...props}
                      aria-label={cellLabel(cell)}
                      aria-pressed={cell.date === selected}
                      onClick={() => onSelect(cell.date)}
                    >
                      {cell.status === 'missed' ? <span className="heat-x" aria-hidden="true">×</span> : null}
                    </button>
                  ) : (
                    <span key={cell.date} {...props}>
                      {cell.status === 'missed' ? <span className="heat-x" aria-hidden="true">×</span> : null}
                    </span>
                  )
                })}
              </div>
            ))}
          </div>
        </div>
      </div>
      <div className="heatmap-legend">
        <span className="legend-item">
          <span className="legend-ramp" aria-hidden="true">
            <i data-level="0" />
            <i data-level="1" />
            <i data-level="2" />
            <i data-level="3" />
            <i data-level="4" />
          </span>
          少 → 多
        </span>
        <span className="legend-item">
          <i className="legend-cell" data-status="skipped" aria-hidden="true" />
          跳过
        </span>
        <span className="legend-item">
          <i className="legend-cell" data-status="missed" aria-hidden="true">
            ×
          </i>
          缺失
        </span>
        <span className="legend-item">
          <i className="legend-cell" data-status="rest" aria-hidden="true" />
          休息
        </span>
      </div>
    </div>
  )
}

/**
 * A sparkline: one hue, 2px line, a whisper of area fill and a dot on the
 * latest value. For stat tiles and the hero — magnitude at a glance, no axes.
 */
export function Sparkline({
  values,
  width = 120,
  height = 34,
  label,
}: {
  values: number[]
  width?: number
  height?: number
  label?: string
}) {
  const max = Math.max(1, ...values)
  const n = values.length
  const points = values.map((v, i) => {
    const x = n <= 1 ? width / 2 : (i / (n - 1)) * (width - 8) + 4
    const y = height - 5 - (v / max) * (height - 12)
    return { x, y }
  })
  const line = points.map((p) => `${p.x},${p.y}`).join(' ')
  const last = points[points.length - 1]
  return (
    <svg
      className="spark"
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      role="img"
      aria-label={label ?? '趋势'}
    >
      {n > 1 ? (
        <polygon points={`4,${height - 2} ${line} ${width - 4},${height - 2}`} fill="var(--ink, var(--brand))" opacity="0.09" />
      ) : null}
      <polyline points={line} fill="none" stroke="var(--ink, var(--brand))" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
      {last !== undefined ? <circle cx={last.x} cy={last.y} r="2.6" fill="var(--ink, var(--brand))" /> : null}
    </svg>
  )
}

export type DonutSlice = {
  key: string
  label: string
  icon: string
  color: InkColor
  value: number
}

/**
 * A donut of category shares: one arc per category in its own ink, a 3px
 * surface gap between arcs, the total in the hole, and a labelled legend
 * beside it — identity never rests on color alone.
 */
export function Donut({
  slices,
  size = 148,
  thickness = 16,
  centerLabel = '次',
}: {
  slices: DonutSlice[]
  size?: number
  thickness?: number
  centerLabel?: string
}) {
  const total = slices.reduce((sum, s) => sum + s.value, 0)
  const radius = (size - thickness) / 2
  const circumference = 2 * Math.PI * radius
  const gap = 3
  // Cumulative starts, computed purely (no render-scope mutation).
  const shares = slices.map((s) => (total > 0 ? s.value / total : 0))
  const starts = shares.map((_, i) => shares.slice(0, i).reduce((a, b) => a + b, 0))
  const wrapStyle = useMemo(() => ({ width: size, height: size }), [size])
  return (
    <div className="donut" style={wrapStyle}>
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} aria-hidden="true">
        <circle cx={size / 2} cy={size / 2} r={radius} fill="none" stroke="var(--sunken)" strokeWidth={thickness} />
        {total > 0
          ? slices.map((s, i) => {
              const len = Math.max(0.5, (shares[i] ?? 0) * circumference - gap)
              return (
                <circle
                  key={s.key}
                  data-ink={s.color}
                  cx={size / 2}
                  cy={size / 2}
                  r={radius}
                  fill="none"
                  stroke="var(--ink)"
                  strokeWidth={thickness}
                  strokeDasharray={`${len} ${circumference - len}`}
                  strokeDashoffset={-(starts[i] ?? 0) * circumference}
                  transform={`rotate(-90 ${size / 2} ${size / 2})`}
                >
                  <title>{`${s.label} · ${s.value} ${centerLabel}`}</title>
                </circle>
              )
            })
          : null}
      </svg>
      <span className="donut-center">
        <strong>{total}</strong>
        <span>{centerLabel}</span>
      </span>
    </div>
  )
}

/**
 * The trend: one bar per period against a dashed target line. Single hue,
 * 4px rounded data-ends, 2px gaps, selective direct labels (latest and best),
 * a tooltip on every bar.
 */
export function TrendBars({
  bars,
  color,
  unit,
  labelFor,
}: {
  bars: PeriodBar[]
  color: ChartColor
  unit: string
  labelFor: (bar: PeriodBar) => string
}) {
  const max = Math.max(1, ...bars.map((b) => Math.max(b.value, b.target)))
  const bestIndex = bars.reduce((best, bar, i) => (bar.value > (bars[best]?.value ?? 0) ? i : best), 0)
  // Styles are memoized with the rows so chart props keep stable identities.
  const rows = useMemo(
    () =>
      bars.map((bar, index) => ({
        bar,
        title: `${formatRangeCN(bar.start, bar.end)}：${formatValue(bar.value)} / ${formatValue(bar.target)} ${unit} · ${STATUS_LABEL[bar.status]}`,
        showLabel: index === bars.length - 1 || (index === bestIndex && bar.value > 0),
        targetStyle: { bottom: `${Math.round((bar.target / max) * 100)}%` },
        barStyle: { height: `${Math.max(bar.value > 0 ? 4 : 0, Math.round((bar.value / max) * 100))}%` },
      })),
    [bars, max, bestIndex, unit],
  )
  return (
    <div className="trend" data-ink={color}>
      <div className="trend-plot">
        {rows.map(({ bar, title, showLabel, targetStyle, barStyle }) => (
          <button key={bar.key} type="button" className="trend-col" title={title} aria-label={title}>
            <span className="trend-value" data-show={showLabel ? 'true' : undefined}>
              {bar.value > 0 ? formatValue(bar.value) : ''}
            </span>
            <span className="trend-track">
              {bar.target > 0 ? <span className="trend-target" style={targetStyle} aria-hidden="true" /> : null}
              <span className="trend-bar" data-status={bar.status} style={barStyle} />
            </span>
            <span className="trend-label">{labelFor(bar)}</span>
          </button>
        ))}
      </div>
    </div>
  )
}
