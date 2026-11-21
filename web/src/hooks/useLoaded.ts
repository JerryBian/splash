import { use, useMemo } from 'react'
import { load, type Loaded } from '../lib/async'

/**
 * Reads a cached request with React 19's use(): the component suspends until
 * the promise settles, and the failure arrives as a value, never a throw.
 *
 * The memo depends on the key alone, on purpose: the key fully identifies the
 * request (including its version), and depending on the fetch closure would
 * mint a new promise every render — use() would then suspend forever.
 */
export function useLoaded<T>(key: string, fetch: () => Promise<T>): Loaded<T> {
  // oxlint-disable-next-line react-hooks/exhaustive-deps -- key identifies the request; see above
  const promise = useMemo(() => load(key, fetch), [key])
  return use(promise)
}
