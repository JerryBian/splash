/**
 * Browser notification plumbing for daily reminders.
 *
 * Permission is never requested on page load: browsers increasingly ignore (or
 * penalise) a prompt that is not tied to a user gesture, so the "我的" page
 * asks from a click. The preference is stored locally because it is a property
 * of this device, not of the account.
 */

const STORAGE_KEY = 'splash.notifications'

/** A Notification permission, or "unsupported" when the API is missing. */
export type NotificationSupport = NotificationPermission | 'unsupported'

/** True when this browser exposes the Notification API. */
export function notificationsSupported(): boolean {
  return typeof window !== 'undefined' && 'Notification' in window
}

/** The current permission without prompting. */
export function notificationPermission(): NotificationSupport {
  if (!notificationsSupported()) return 'unsupported'
  return Notification.permission
}

/** Whether the user asked for browser notifications on this device. */
export function loadNotificationsEnabled(): boolean {
  try {
    return window.localStorage.getItem(STORAGE_KEY) === 'on'
  } catch {
    // Private mode or a disabled storage partition: notifications stay off,
    // which is the safe default.
    return false
  }
}

/** Persists the browser-notification preference. */
export function saveNotificationsEnabled(enabled: boolean): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, enabled ? 'on' : 'off')
  } catch {
    // Best effort: the in-memory state still drives this session.
  }
}

/**
 * Requests notification permission. Must be called from a user gesture; the
 * caller is the settings switch, not an effect.
 */
export async function requestNotificationPermission(): Promise<NotificationSupport> {
  if (!notificationsSupported()) return 'unsupported'
  try {
    return await Notification.requestPermission()
  } catch {
    return notificationPermission()
  }
}

/**
 * Shows one browser notification. Returns false when notifications are not
 * granted or the constructor throws (some embedded webviews), so the caller can
 * fall back to the in-app toast it shows anyway.
 */
export function showBrowserNotification(title: string, body: string): boolean {
  if (notificationPermission() !== 'granted') return false
  try {
    const notification = new Notification(title, { body })
    notification.addEventListener('click', () => {
      window.focus()
      notification.close()
    })
    return true
  } catch {
    return false
  }
}
