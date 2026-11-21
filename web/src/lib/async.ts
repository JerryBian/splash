import type { Checkin } from '../api/types'
import { api } from '../api/client'

/**
 * A tiny promise cache for use()-based loading. Keys fully identify a request;
 * the cached promise is stable across renders (which is what use() needs) and
 * shared between components asking for the same day. Failures are caught into
 * a discriminated result so use() never throws and no error boundary is
 * required — the same contract as the bootstrap load.
 */

export type Loaded<T> = { ok: true; data: T } | { ok: false; error: unknown }

const cache = new Map<string, Promise<Loaded<unknown>>>()

export function load<T>(key: string, fetch: () => Promise<T>): Promise<Loaded<T>> {
  const existing = cache.get(key)
  if (existing !== undefined) return existing as Promise<Loaded<T>>
  const promise = fetch()
    .then((data): Loaded<T> => ({ ok: true, data }))
    .catch((error: unknown): Loaded<T> => ({ ok: false, error }))
  // Bounded: keys carry a version counter, so old entries are dead weight.
  if (cache.size > 60) cache.clear()
  cache.set(key, promise)
  return promise
}

/** Drops cached requests under a prefix, so the next read refetches. */
export function invalidate(prefix: string): void {
  // Deleting from a Map while iterating its keys is safe per the JS spec.
  for (const key of cache.keys()) {
    if (key.startsWith(prefix)) cache.delete(key)
  }
}

export type DayData = {
  records: Checkin[]
  skipped: boolean
}

/** One calendar day of a project: its records and whether it is excused. */
export function fetchDay(projectId: string, day: string): Promise<DayData> {
  return Promise.all([
    api.listCheckins(projectId, { from: day, to: day, limit: 100 }),
    api.listSkips(projectId),
  ]).then(([records, skips]) => ({
    records,
    skipped: skips.some((s) => s.date === day),
  }))
}
