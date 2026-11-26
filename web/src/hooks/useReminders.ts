import { useEffect, useRef } from 'react'
import type { Project } from '../api/types'
import { showBrowserNotification } from '../lib/notify'
import { dueReminders, loadReminderKeys, reminderDayKey, saveReminderKeys } from '../lib/reminders'
import { useApp } from './useApp'

/**
 * Daily reminders while the app is open.
 *
 * The in-app toast always fires, so a reminder works without any permission.
 * The browser notification is opt-in (the "我的" page asks from a click) and
 * keeps working when the tab is in the background. At most one reminder per
 * project per local day: the fired keys live in localStorage, so a reload in
 * the same grace window does not toast again.
 */
export function useReminders(projects: Project[], browserNotifications: boolean): void {
  const { toast } = useApp()
  const fired = useRef<Set<string> | null>(null)

  useEffect(() => {
    if (!projects.some((p) => p.reminderTime !== '' && !p.archived)) return
    const tick = () => {
      const now = new Date()
      const day = reminderDayKey(now)
      if (fired.current === null) {
        // Ignore yesterday's keys: the log is per local day by design.
        fired.current = new Set(loadReminderKeys().filter((key) => key.startsWith(`${day}:`)))
      }
      const due = dueReminders(projects, now, fired.current)
      if (due.length === 0) return
      for (const { project, key } of due) {
        fired.current.add(key)
        toast(`提醒：该「${project.name}」了（${project.reminderTime}）`, 'info')
        if (browserNotifications) {
          showBrowserNotification('水花打卡', `该「${project.name}」了（${project.reminderTime}）`)
        }
      }
      saveReminderKeys([...fired.current].filter((key) => key.startsWith(`${day}:`)))
    }
    tick()
    // 20s keeps the worst-case lag well inside the grace window without a
    // timer that wakes the CPU every second.
    const timer = window.setInterval(tick, 20_000)
    return () => window.clearInterval(timer)
  }, [projects, toast, browserNotifications])
}
