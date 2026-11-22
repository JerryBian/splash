import { describe, expect, it } from 'vitest'
import { INK_COLORS } from '../api/types'
import { pickUnusedColor } from './palette'

describe('pickUnusedColor', () => {
  it('prefers a color that is not already in use', () => {
    const used = INK_COLORS.slice(0, 15)
    const picked = pickUnusedColor(used, () => 0)
    expect(used).not.toContain(picked)
    expect(picked).toBe(INK_COLORS[15])
  })

  it('uses the least-used colors once the palette is exhausted', () => {
    const used = [...INK_COLORS, ...INK_COLORS, 'red']
    const picked = pickUnusedColor(used, () => 0)
    // Red appears three times; the other inks appear twice. The deterministic
    // first candidate is therefore not red.
    expect(picked).not.toBe('red')
  })

  it('survives an empty or unknown history', () => {
    expect(INK_COLORS).toContain(pickUnusedColor([], () => 0))
    expect(INK_COLORS).toContain(pickUnusedColor(['not-an-ink'], () => 0.5))
  })
})
