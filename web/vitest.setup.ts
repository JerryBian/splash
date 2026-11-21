import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach, vi } from 'vitest'

// React Testing Library renders into a container attached to document.body.
// Without this, every test in a file sees the markup of all the tests before it.
afterEach(() => {
  cleanup()
})

// jsdom has no matchMedia; the theme module and the splatter both ask it about
// prefers-color-scheme / prefers-reduced-motion. Answer "light, motion ok".
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: vi.fn().mockImplementation((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })),
})

// jsdom does not implement scrolling; the router resets scroll on tab change.
window.scrollTo = vi.fn() as unknown as typeof window.scrollTo
