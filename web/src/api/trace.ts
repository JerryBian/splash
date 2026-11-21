import { useSyncExternalStore } from 'react'

/**
 * Records the most recent API round trip so the UI can show it.
 *
 * This exists because the interesting thing about this project is not the data
 * on screen — it is that a browser just talked to a single Go binary that has
 * the frontend compiled into it. The masthead draws the real round trip: the
 * method, the status, the measured latency and the X-Request-ID that the Go
 * middleware minted. Nothing here is simulated.
 *
 * Only the latest request is kept. Overlapping requests are possible in
 * principle, and the last one to settle wins; that is the right trade for a
 * status readout and avoids inventing a request log nobody asked for.
 */

export type TraceState = 'idle' | 'sending' | 'settled' | 'failed'

export type Trace = {
  state: TraceState
  method: string
  path: string
  /** HTTP status, or null before the response arrived or if the network failed. */
  status: number | null
  durationMs: number | null
  /** Value of the X-Request-ID response header, echoed by the Go middleware. */
  requestId: string | null
}

const IDLE: Trace = {
  state: 'idle',
  method: '',
  path: '',
  status: null,
  durationMs: null,
  requestId: null,
}

let current: Trace = IDLE
const listeners = new Set<() => void>()

function emit(next: Trace): void {
  current = next
  for (const listener of listeners) listener()
}

function elapsedSince(startedAt: number): number {
  return Math.max(0, Math.round(performance.now() - startedAt))
}

/** Called immediately before fetch. */
export function traceStart(method: string, path: string): void {
  emit({ ...IDLE, state: 'sending', method, path })
}

/** Called when a response arrives, whatever its status. */
export function traceSettled(
  method: string,
  path: string,
  status: number,
  requestId: string | null,
  startedAt: number,
): void {
  emit({
    state: 'settled',
    method,
    path,
    status,
    requestId,
    durationMs: elapsedSince(startedAt),
  })
}

/** Called when fetch itself threw, i.e. the server never answered. */
export function traceFailed(method: string, path: string, startedAt: number): void {
  emit({
    state: 'failed',
    method,
    path,
    status: null,
    requestId: null,
    durationMs: elapsedSince(startedAt),
  })
}

/** Resets the readout. Used by tests. */
export function traceReset(): void {
  emit(IDLE)
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

// getSnapshot must return a stable reference between renders, which is why
// `current` is replaced wholesale rather than mutated.
function getSnapshot(): Trace {
  return current
}

/** Reads the latest round trip without subscribing. Used by tests. */
export function readTrace(): Trace {
  return current
}

/** Subscribes a component to the latest round trip. */
export function useLastTrace(): Trace {
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot)
}
