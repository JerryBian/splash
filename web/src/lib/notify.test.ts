import { describe, expect, it } from 'vitest'
import {
  loadNotificationsEnabled,
  notificationPermission,
  saveNotificationsEnabled,
  showBrowserNotification,
} from './notify'

describe('notification settings', () => {
  it('persists the per-device preference', () => {
    saveNotificationsEnabled(true)
    expect(loadNotificationsEnabled()).toBe(true)
    saveNotificationsEnabled(false)
    expect(loadNotificationsEnabled()).toBe(false)
  })

  it('reports unsupported gracefully when the API is absent', () => {
    // jsdom does not implement Notification; the helpers must degrade to a
    // no-op rather than throw during startup.
    expect(['unsupported', 'default', 'denied', 'granted']).toContain(notificationPermission())
    expect(showBrowserNotification('title', 'body')).toBe(false)
  })
})
