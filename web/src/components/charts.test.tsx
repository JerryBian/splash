import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import type { HeatCell, PeriodBar } from '../api/types'
import { Donut, Heatmap, ProgressRing, Sparkline, TrendBars } from './charts'

const cells: HeatCell[] = [
  { date: '2026-11-19', value: 0, count: 0, status: 'skipped' },
  { date: '2026-11-20', value: 0, count: 0, status: 'missed' },
  { date: '2026-11-21', value: 8, count: 1, status: 'done' },
]

describe('Heatmap', () => {
  it('labels every cell with date, status and amount', () => {
    render(<Heatmap cells={cells} color="blue" dailyTarget={8} unit="杯" />)
    expect(screen.getByTitle('11月21日：已完成，8杯')).toBeInTheDocument()
    expect(screen.getByTitle('11月19日：已跳过')).toBeInTheDocument()
  })

  it('encodes status by shape, not color alone', () => {
    const { container } = render(<Heatmap cells={cells} color="blue" dailyTarget={8} />)
    const missed = container.querySelector('[data-status="missed"]')
    expect(missed?.textContent).toBe('×')
    expect(container.querySelector('[data-status="skipped"]')).toBeInTheDocument()
    expect(container.querySelector('[data-level="4"]')).toBeInTheDocument()
    // The legend names every marker.
    expect(screen.getByText('跳过')).toBeInTheDocument()
    expect(screen.getByText('缺失')).toBeInTheDocument()
    expect(screen.getByText('休息')).toBeInTheDocument()
  })

  it('calls onSelect with the tapped day', async () => {
    const onSelect = vi.fn()
    render(<Heatmap cells={cells} color="blue" dailyTarget={8} unit="杯" onSelect={onSelect} />)
    await userEvent.click(screen.getByLabelText('11月21日：已完成，8杯'))
    expect(onSelect).toHaveBeenCalledWith('2026-11-21')
  })

  it('renders without interaction when onSelect is absent', () => {
    const { container } = render(<Heatmap cells={cells} color="blue" dailyTarget={8} />)
    expect(container.querySelectorAll('button.heat-cell')).toHaveLength(0)
  })
})

// Module scope on purpose: react-perf flags arrays created in the same scope
// as the JSX they are passed to.
const bars: PeriodBar[] = [
  { key: '2026-11-09', start: '2026-11-09', end: '2026-11-15', value: 1, target: 3, status: 'missed' },
  { key: '2026-11-16', start: '2026-11-16', end: '2026-11-22', value: 3, target: 3, status: 'done' },
]
const sparkValues = [1, 4, 2, 6]
const sparkSingle = [3]
const sparkEmpty: number[] = []
const donutSlices = [
  { key: 'a', label: '健康', icon: '💪', color: 'green' as const, value: 60 },
  { key: 'b', label: '学习', icon: '📖', color: 'blue' as const, value: 40 },
]
const donutEmpty: typeof donutSlices = []

describe('TrendBars', () => {
  it('gives every bar a tooltip with value, target and verdict', () => {
    render(<TrendBars bars={bars} color="green" unit="次" labelFor={(b) => b.start.slice(5)} />)
    expect(screen.getByLabelText('11月9日 – 11月15日：1 / 3 次 · 缺失')).toBeInTheDocument()
    expect(screen.getByLabelText('11月16日 – 11月22日：3 / 3 次 · 已完成')).toBeInTheDocument()
  })

  it('draws the dashed target line', () => {
    const { container } = render(
      <TrendBars bars={bars} color="green" unit="次" labelFor={(b) => b.start.slice(5)} />,
    )
    expect(container.querySelectorAll('.trend-target')).toHaveLength(2)
  })
})

describe('Sparkline', () => {
  it('draws one point per value and dots the latest', () => {
    const { container } = render(<Sparkline values={sparkValues} label="测试趋势" />)
    const line = container.querySelector('polyline') as SVGPolylineElement
    expect(line.getAttribute('points')?.split(' ')).toHaveLength(4)
    expect(container.querySelectorAll('circle')).toHaveLength(1)
    expect(screen.getByLabelText('测试趋势')).toBeInTheDocument()
  })

  it('survives a single value and an empty series', () => {
    const { container } = render(<Sparkline values={sparkSingle} />)
    expect(container.querySelector('polyline')).toBeInTheDocument()
    const empty = render(<Sparkline values={sparkEmpty} />)
    expect(empty.container.querySelector('polyline')).toBeInTheDocument()
  })
})

describe('Donut', () => {
  it('draws one arc per slice with a surface gap and the total in the hole', () => {
    const { container } = render(<Donut slices={donutSlices} centerLabel="次" />)
    const arcs = container.querySelectorAll('circle[data-ink]')
    expect(arcs).toHaveLength(2)
    const first = arcs[0] as SVGCircleElement
    const parts = (first.getAttribute('stroke-dasharray') ?? '').split(' ').map(Number)
    const len = parts[0] ?? 0
    const rest = parts[1] ?? 0
    const circumference = 2 * Math.PI * 66
    // 60% of the circle minus the 3px gap.
    expect(len).toBeCloseTo(0.6 * circumference - 3, 0)
    expect(len + rest).toBeCloseTo(circumference, 0)
    expect(screen.getByText('100')).toBeInTheDocument()
  })

  it('labels every arc for tooltips', () => {
    const { container } = render(<Donut slices={donutSlices} />)
    const titles = [...container.querySelectorAll('circle[data-ink] title')].map((t) => t.textContent)
    expect(titles).toEqual(['健康 · 60 次', '学习 · 40 次'])
  })

  it('renders an empty track when there is nothing to share', () => {
    const { container } = render(<Donut slices={donutEmpty} />)
    expect(container.querySelectorAll('circle[data-ink]')).toHaveLength(0)
    expect(screen.getByText('0')).toBeInTheDocument()
  })
})

describe('ProgressRing', () => {
  it('shows the fraction and arcs to the ratio', () => {
    const { container } = render(
      <ProgressRing value={3} max={4} size={64} stroke={6}>
        <span>3/4</span>
      </ProgressRing>,
    )
    expect(screen.getByText('3/4')).toBeInTheDocument()
    const arc = container.querySelector('.ring-arc') as SVGCircleElement
    const circumference = 2 * Math.PI * 29
    expect(Number(arc.getAttribute('stroke-dashoffset'))).toBeCloseTo(circumference * 0.25, 1)
  })

  it('clamps beyond the target and survives a zero max', () => {
    const { container } = render(
      <ProgressRing value={9} max={4}>
        <span>x</span>
      </ProgressRing>,
    )
    const arc = container.querySelector('.ring-arc') as SVGCircleElement
    expect(Number(arc.getAttribute('stroke-dashoffset'))).toBe(0)
    render(
      <ProgressRing value={0} max={0}>
        <span>y</span>
      </ProgressRing>,
    )
    expect(screen.getByText('y')).toBeInTheDocument()
  })
})
