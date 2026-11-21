#!/usr/bin/env node
/**
 * Runs both halves of the project in one terminal:
 *
 *   node scripts/dev.mjs
 *
 * The Go server listens on 127.0.0.1:8080 and serves the API. Vite serves the
 * frontend on :5173 with hot reload and proxies /api to the Go server, so the
 * browser only ever talks to one origin and no CORS configuration is needed.
 *
 * Work on the frontend in the Vite tab; the Go binary picks up frontend changes
 * only when you rebuild it (`node scripts/build.mjs web`).
 *
 * Output from both processes is interleaved with a prefix, and Ctrl+C stops
 * both. If either one exits, the other is stopped too — a half-running stack is
 * more confusing than no stack.
 */

import { spawn } from 'node:child_process'
import { existsSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { env, exit, platform as hostPlatform, stdin } from 'node:process'
import { fileURLToPath } from 'node:url'

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const IS_WINDOWS = hostPlatform === 'win32'
const API_ADDR = env.SPLASH_HTTP_ADDR || '127.0.0.1:8080'

const COLOURS = { go: '\x1b[36m', web: '\x1b[35m', dev: '\x1b[90m', reset: '\x1b[0m' }

function say(message) {
  process.stdout.write(`${COLOURS.dev}[dev]${COLOURS.reset} ${message}\n`)
}

/**
 * Starts a process and echoes its output with a prefix.
 *
 * The package manager needs a shell on Windows because pnpm and npm are .cmd
 * shims there; go does not, and running it directly keeps argument handling
 * identical on every platform.
 */
function start(name, command, args, options = {}) {
  const child = spawn(command, args, {
    cwd: ROOT,
    env: { ...env, ...options.env },
    stdio: ['ignore', 'pipe', 'pipe'],
    shell: options.shell === true && IS_WINDOWS,
  })

  const prefix = `${COLOURS[name] ?? ''}[${name}]${COLOURS.reset} `
  for (const stream of [child.stdout, child.stderr]) {
    let pending = ''
    stream.setEncoding('utf8')
    stream.on('data', (chunk) => {
      pending += chunk
      const lines = pending.split('\n')
      pending = lines.pop() ?? ''
      for (const line of lines) {
        if (line.trim() !== '') process.stdout.write(prefix + line + '\n')
      }
    })
    stream.on('end', () => {
      if (pending.trim() !== '') process.stdout.write(prefix + pending + '\n')
    })
  }

  child.on('error', (error) => {
    say(`could not start ${name}: ${error.message}`)
    shutdown(1)
  })

  return child
}

const children = []
let shuttingDown = false

function shutdown(code) {
  if (shuttingDown) return
  shuttingDown = true
  for (const child of children) {
    if (child.exitCode === null && child.signalCode === null) child.kill()
  }
  // Give children a moment to exit before the parent disappears, otherwise
  // an orphaned dev server keeps holding the port.
  setTimeout(() => exit(code), 100).unref()
}

process.on('SIGINT', () => {
  say('stopping')
  shutdown(0)
})
process.on('SIGTERM', () => shutdown(0))
// Ctrl+C in a Windows console arrives as SIGINT for the parent only; the
// children are killed above. Reading stdin keeps the parent attached to the
// console so the signal is delivered at all.
if (IS_WINDOWS) stdin.resume()

const packageManager = existsSync(join(ROOT, 'web', 'pnpm-lock.yaml')) ? 'pnpm' : 'npm'

say(`Go API    → http://${API_ADDR}`)
say(`Frontend  → http://localhost:5173 (proxies /api to the Go server)`)

children.push(
  start('go', env.GO || 'go', ['run', './cmd/server'], {
    env: {
      SPLASH_HTTP_ADDR: API_ADDR,
      SPLASH_ENV: 'development',
      SPLASH_LOG_FORMAT: 'text',
      SPLASH_LOG_LEVEL: env.SPLASH_LOG_LEVEL || 'debug',
    },
  }),
)

children.push(
  start('web', packageManager, ['--dir', 'web', 'run', 'dev'], { shell: true }),
)

for (const child of children) {
  child.on('exit', (code, signal) => {
    if (shuttingDown) return
    say(`a process exited (code=${code} signal=${signal}); stopping the other`)
    shutdown(code ?? 1)
  })
}
