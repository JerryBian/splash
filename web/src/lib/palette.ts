import { INK_COLORS, type InkColor } from '../api/types'

/**
 * Pick a color for a new project/category without repeating the palette that
 * is already in use. When every ink is taken, prefer the least-used ones
 * instead of falling back to one fixed color.
 *
 * The random function is injectable so callers can be deterministic in tests;
 * in the app it is Math.random.
 */
export function pickUnusedColor(
  used: readonly string[],
  random: () => number = Math.random,
): InkColor {
  const counts = new Map<InkColor, number>()
  for (const color of INK_COLORS) counts.set(color, 0)
  for (const color of used) {
    if (counts.has(color as InkColor)) {
      counts.set(color as InkColor, (counts.get(color as InkColor) ?? 0) + 1)
    }
  }

  const unused = INK_COLORS.filter((color) => (counts.get(color) ?? 0) === 0)
  const pool =
    unused.length > 0
      ? unused
      : (() => {
          let lowest = Number.POSITIVE_INFINITY
          for (const count of counts.values()) lowest = Math.min(lowest, count)
          return INK_COLORS.filter((color) => (counts.get(color) ?? 0) === lowest)
        })()

  const index = Math.floor(random() * pool.length)
  return pool[index] ?? INK_COLORS[0] ?? 'blue'
}
