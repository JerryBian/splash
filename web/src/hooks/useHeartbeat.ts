import { useEffect } from 'react'
import { api } from '../api/client'

/**
 * Keeps the session alive while an editing surface is open. The server slides
 * the idle timeout on any authenticated request; a quiet ping every few
 * minutes means "正在编辑就不会超时" even when the user is typing without
 * saving. Failures are swallowed: a dead session surfaces on the next real
 * mutation, not as a heartbeat error spam.
 */
export function useSessionHeartbeat(active: boolean, intervalMs = 4 * 60 * 1000): void {
  useEffect(() => {
    if (!active) return
    const timer = window.setInterval(() => {
      void api.me().catch(() => {})
    }, intervalMs)
    return () => window.clearInterval(timer)
  }, [active, intervalMs])
}
