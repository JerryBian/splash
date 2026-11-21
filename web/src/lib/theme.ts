/**
 * Theme preference: light, dark, or follow the OS. The choice is a per-viewer
 * convenience, so localStorage is the right home; every access is wrapped
 * because storage can throw in private windows or hardened browsers, and the
 * app must render correctly without it.
 */

export type ThemeChoice = 'light' | 'dark' | 'system'

const KEY = 'splash-theme'

export function loadTheme(): ThemeChoice {
  try {
    const raw = localStorage.getItem(KEY)
    if (raw === 'light' || raw === 'dark' || raw === 'system') return raw
  } catch {
    // storage unavailable — fall through to the default
  }
  return 'system'
}

export function saveTheme(choice: ThemeChoice): void {
  try {
    localStorage.setItem(KEY, choice)
  } catch {
    // the preference just won't persist; applying it still works
  }
}

/**
 * Applies the choice to <html>. "system" removes the attribute so the CSS
 * prefers-color-scheme media query decides; explicit choices override it.
 */
export function applyTheme(choice: ThemeChoice): void {
  const root = document.documentElement
  if (choice === 'system') {
    delete root.dataset.theme
  } else {
    root.dataset.theme = choice
  }
  const dark =
    choice === 'dark' ||
    (choice === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  const meta = document.querySelector('meta[name="theme-color"]')
  if (meta !== null) meta.setAttribute('content', dark ? '#14161a' : '#f4f4f1')
}

/** Subscribes to OS theme changes while in "system" mode. Returns an unsubscribe. */
export function watchSystemTheme(onChange: () => void): () => void {
  const mq = window.matchMedia('(prefers-color-scheme: dark)')
  mq.addEventListener('change', onChange)
  return () => mq.removeEventListener('change', onChange)
}
