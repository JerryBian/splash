import { useEffect, useState } from 'react'

/**
 * A ticking clock for countdowns. The interval only runs while `active` is
 * true, so pages without a waiting interval project carry no timer at all.
 */
export function useNow(active: boolean, intervalMs = 1000): Date {
  const [now, setNow] = useState(() => new Date())
  useEffect(() => {
    if (!active) return
    const timer = window.setInterval(() => setNow(new Date()), intervalMs)
    return () => window.clearInterval(timer)
  }, [active, intervalMs])
  return now
}

/**
 * Hash routing: #today, #stats, #manage, #me, #project/<id>, #edit/<id|new>.
 * The hash gives the mobile back button real work to do — overlays close on
 * back instead of leaving the app. Login is a full-screen gate above the
 * router, not a hash route.
 */
export type Route =
  | { name: 'today' }
  | { name: 'stats' }
  | { name: 'manage' }
  | { name: 'me' }
  | { name: 'project'; id: string }
  | { name: 'edit'; id: string }

export function parseHash(hash: string): Route {
  const raw = hash.replace(/^#\/?/, '')
  const [head, param] = raw.split('/')
  switch (head) {
    case 'stats':
      return { name: 'stats' }
    case 'manage':
      return { name: 'manage' }
    case 'me':
      return { name: 'me' }
    case 'project':
      return param ? { name: 'project', id: param } : { name: 'today' }
    case 'edit':
      return param ? { name: 'edit', id: param } : { name: 'today' }
    default:
      return { name: 'today' }
  }
}

export function navigate(hash: string): void {
  window.location.hash = hash
}

export function useHashRoute(): Route {
  const [route, setRoute] = useState<Route>(() => parseHash(window.location.hash))
  useEffect(() => {
    const onChange = () => setRoute(parseHash(window.location.hash))
    window.addEventListener('hashchange', onChange)
    return () => window.removeEventListener('hashchange', onChange)
  }, [])
  return route
}
