#!/usr/bin/env node
/**
 * Cross-platform build orchestrator.
 *
 * Everything the release pipeline does lives here in Node rather than in shell,
 * for one reason: the repository has to build the same way on a Windows laptop,
 * a macOS laptop and a Linux CI runner, and `make` is not installed on Windows
 * by default. The Makefile is a thin wrapper over these scripts, so every step
 * has exactly one implementation.
 *
 * Usage:
 *   node scripts/build.mjs              # frontend + host binary into bin/
 *   node scripts/build.mjs web          # frontend only, into internal/web/dist
 *   node scripts/build.mjs binary       # host binary only (assumes web is built)
 *   node scripts/build.mjs release      # every platform, archives + checksums
 *   node scripts/build.mjs clean
 *   node scripts/build.mjs status
 *
 * Environment:
 *   SPLASH_VERSION    version to stamp (default: git describe, else "dev")
 *   SPLASH_INSTALL    "false" skips the dependency install step
 *   OUT_DIR           where `release` writes artifacts (default: dist/)
 *   GO, PNPM          override the tool binaries
 */

import { spawn } from 'node:child_process'
import { createHash } from 'node:crypto'
import { createReadStream, existsSync } from 'node:fs'
import { cp, mkdir, readdir, rm, writeFile } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { arch as hostArch, env, exit, platform as hostPlatform } from 'node:process'
import { fileURLToPath } from 'node:url'

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const MODULE = 'github.com/JerryBian/splash'
const BINARY = 'splash'
const WEB_DIR = join(ROOT, 'web')
const WEB_DIST = join(WEB_DIR, 'dist')
const EMBED_DIR = join(ROOT, 'internal', 'web', 'dist')
const KEEP_FILE = '.gitkeep'
const KEEP_CONTENTS = `# This file exists so that \`go:embed all:dist\` has something to embed on a fresh
# clone. \`node scripts/build.mjs\` (or \`make build\`) copies the Vite output here
# and preserves this file. It is the only thing in the directory that git tracks.
`

const GO = env.GO || 'go'
const IS_WINDOWS = hostPlatform === 'win32'

// Node and Go name the same machines differently, so the host values need
// translating before they can be used as GOOS/GOARCH.
const GOOS_BY_PLATFORM = { win32: 'windows', darwin: 'darwin', linux: 'linux', freebsd: 'freebsd' }
const GOARCH_BY_ARCH = { x64: 'amd64', ia32: '386', arm64: 'arm64', arm: 'arm', ppc64: 'ppc64' }

/**
 * Platforms the release pipeline builds for. The Docker images cover the five
 * Linux targets that distroless supports; the extra 386 / ARMv7 archives are
 * for people running the binary directly on old or small hardware.
 */
const TARGETS = [
  { goos: 'linux', goarch: 'amd64' },
  { goos: 'linux', goarch: 'arm64' },
  { goos: 'linux', goarch: '386' },
  { goos: 'linux', goarch: 'arm', goarm: '7' },
  { goos: 'linux', goarch: 'ppc64le' },
  { goos: 'linux', goarch: 's390x' },
  { goos: 'darwin', goarch: 'amd64' },
  { goos: 'darwin', goarch: 'arm64' },
  { goos: 'windows', goarch: 'amd64' },
  { goos: 'windows', goarch: 'arm64' },
]

function log(message) {
  process.stdout.write(`[build] ${message}\n`)
}

function fail(message) {
  process.stderr.write(`[build] ${message}\n`)
  exit(1)
}

/**
 * Runs a command, streaming its output.
 *
 * Tools are spawned directly, with no shell, so arguments containing spaces
 * (the -ldflags value, output paths) need no per-platform quoting rules.
 *
 * The package manager is the exception: on Windows pnpm and npm are .cmd shims,
 * which Node refuses to spawn without a shell (EINVAL, since the fix for
 * CVE-2024-27980) while shell:true with an argument vector is deprecated
 * (DEP0190). Invoking cmd.exe explicitly with a single pre-quoted command line
 * satisfies both, which is exactly what Node's own shell:true does internally.
 */
function run(command, args, options = {}) {
  const { cwd = ROOT, env: extraEnv, capture = false, shell = false } = options

  let [spawnCommand, spawnArgs, spawnOptions] = [command, args, {}]
  if (shell && IS_WINDOWS) {
    for (const arg of args) {
      // The command line is built by joining, so refuse anything that would
      // need escaping rather than escaping it incorrectly.
      if (/["&|<>^%]/.test(arg)) {
        return Promise.reject(new Error(`refusing to pass ${JSON.stringify(arg)} through cmd.exe`))
      }
    }
    spawnCommand = 'cmd.exe'
    spawnArgs = ['/d', '/s', '/c', `"${[command, ...args].join(' ')}"`]
    spawnOptions = { windowsVerbatimArguments: true }
  }

  return new Promise((resolvePromise, rejectPromise) => {
    const child = spawn(spawnCommand, spawnArgs, {
      cwd,
      env: { ...env, ...extraEnv },
      stdio: capture ? ['ignore', 'pipe', 'pipe'] : ['ignore', 'inherit', 'inherit'],
      ...spawnOptions,
    })

    let stdout = ''
    let stderr = ''
    if (capture) {
      child.stdout.on('data', (chunk) => {
        stdout += chunk
      })
      child.stderr.on('data', (chunk) => {
        stderr += chunk
      })
    }

    child.on('error', rejectPromise)
    child.on('close', (code) => {
      if (code === 0) {
        resolvePromise(stdout.trim())
        return
      }
      const detail = capture && stderr.trim() ? `\n${stderr.trim()}` : ''
      // Quote each argument: a -ldflags value contains spaces, and printing the
      // vector unquoted makes a correct invocation look like a broken one.
      const invocation = [command, ...args.map((a) => JSON.stringify(a))].join(' ')
      rejectPromise(new Error(`${invocation} exited with code ${code}${detail}`))
    })
  })
}

/** Runs git and swallows failure, so builds work outside a repository too. */
async function git(...args) {
  try {
    return await run('git', args, { capture: true })
  } catch {
    return ''
  }
}

/** The version to stamp: an explicit value, else git describe, else "dev". */
async function resolveVersion() {
  if (env.SPLASH_VERSION) return env.SPLASH_VERSION.replace(/^refs\/tags\//, '')
  const described = await git('describe', '--tags', '--always', '--dirty')
  return described || 'dev'
}

/** Build metadata for internal/version, stamped through -ldflags. */
async function buildInfo() {
  const [version, commit] = await Promise.all([
    resolveVersion(),
    git('rev-parse', 'HEAD'),
  ])
  return {
    version,
    commit: commit || 'unknown',
    date: new Date().toISOString().replace(/\.\d+Z$/, 'Z'),
  }
}

function exeName(goos) {
  return goos === 'windows' ? `${BINARY}.exe` : BINARY
}

function ldflags({ version, commit, date }) {
  const prefix = `${MODULE}/internal/version`
  return [
    '-s',
    '-w',
    `-X ${prefix}.version=${version}`,
    `-X ${prefix}.commit=${commit}`,
    `-X ${prefix}.date=${date}`,
  ].join(' ')
}

/** Chooses a package manager, preferring the one the lockfile is for. */
function packageManager() {
  if (env.PNPM) return { command: env.PNPM, lockfile: 'pnpm-lock.yaml' }
  if (existsSync(join(WEB_DIR, 'pnpm-lock.yaml'))) {
    return { command: 'pnpm', lockfile: 'pnpm-lock.yaml' }
  }
  return { command: 'npm', lockfile: 'package-lock.json' }
}

async function installWebDependencies() {
  const { command, lockfile } = packageManager()
  const hasLockfile = existsSync(join(WEB_DIR, lockfile))

  if (command === 'pnpm') {
    // A frozen lockfile is the default everywhere, not just CI: if someone adds
    // a dependency and forgets to commit the lockfile, the build should say so
    // here rather than silently resolving something new.
    const args = hasLockfile ? ['install', '--frozen-lockfile'] : ['install']
    log(`pnpm ${args.join(' ')}`)
    await run(command, args, { cwd: WEB_DIR, shell: true })
    return
  }

  const args = hasLockfile ? ['ci'] : ['install']
  log(`npm ${args[0]}`)
  await run(command, args, { cwd: WEB_DIR, shell: true })
}

/**
 * Replaces the embedded frontend with a fresh build, preserving .gitkeep.
 *
 * The placeholder file is not decoration: `//go:embed all:dist` fails the Go
 * build when the directory is empty, so a clean checkout that never ran the
 * frontend build would not compile without it.
 */
async function syncEmbeddedFrontend() {
  await mkdir(EMBED_DIR, { recursive: true })

  for (const entry of await readdir(EMBED_DIR)) {
    if (entry === KEEP_FILE) continue
    await rm(join(EMBED_DIR, entry), { recursive: true, force: true })
  }
  for (const entry of await readdir(WEB_DIST)) {
    await cp(join(WEB_DIST, entry), join(EMBED_DIR, entry), { recursive: true })
  }

  const keep = join(EMBED_DIR, KEEP_FILE)
  if (!existsSync(keep)) await writeFile(keep, KEEP_CONTENTS)

  const files = await readdir(EMBED_DIR)
  log(`embedded ${files.length - 1} frontend file(s) into internal/web/dist`)
}

async function buildWeb() {
  if (env.SPLASH_INSTALL !== 'false') await installWebDependencies()
  const { command } = packageManager()
  log(`${command} run build`)
  await run(command, ['run', 'build'], { cwd: WEB_DIR, shell: true })
  await syncEmbeddedFrontend()
}

/** Compiles the server for one platform. CGO stays off so binaries are static. */
async function compile({ goos, goarch, goarm, out, info, trim = true }) {
  const args = ['build']
  if (trim) args.push('-trimpath')
  args.push('-ldflags', ldflags(info), '-o', out, './cmd/server')

  log(`go build ${goos}/${goarch}${goarm ? `/v${goarm}` : ''} → ${out}`)
  await run(GO, args, {
    env: {
      CGO_ENABLED: '0',
      GOOS: goos,
      GOARCH: goarch,
      ...(goarm === undefined ? {} : { GOARM: goarm }),
    },
  })
}

async function sha256(path) {
  const hash = createHash('sha256')
  await new Promise((resolvePromise, rejectPromise) => {
    createReadStream(path)
      .on('data', (chunk) => hash.update(chunk))
      .on('end', resolvePromise)
      .on('error', rejectPromise)
  })
  return hash.digest('hex')
}

/**
 * Packs a directory. tar handles both formats: bsdtar ships with Windows 10 and
 * later, GNU tar with every CI image, and `-a` picks the compressor from the
 * file extension, so no separate zip binary is needed anywhere.
 */
async function archive(sourceDir, archivePath) {
  const args = archivePath.endsWith('.zip')
    ? ['-a', '-cf', archivePath, '-C', sourceDir, '.']
    : ['-czf', archivePath, '-C', sourceDir, '.']
  await run('tar', args)
}

/** Frontend once, then one binary per platform, then archives and checksums. */
async function release() {
  const outDir = resolve(ROOT, env.OUT_DIR || 'dist')
  const info = await buildInfo()

  log(`release ${info.version} (${info.commit.slice(0, 7)})`)
  await rm(outDir, { recursive: true, force: true })
  await mkdir(outDir, { recursive: true })
  await buildWeb()

  const stageRoot = join(outDir, 'stage')
  const names = []

  for (const { goos, goarch, goarm } of TARGETS) {
    const platform = `${goarch}${goarm === undefined ? '' : `v${goarm}`}`
    const base = `${BINARY}_${info.version.replace(/^v/, '')}_${goos}_${platform}`
    const stage = join(stageRoot, base)
    await mkdir(stage, { recursive: true })

    await compile({ goos, goarch, goarm, out: join(stage, exeName(goos)), info })

    const extras = ['README.md', 'LICENSE', 'AGENTS.md']
    for (const name of extras) {
      const source = join(ROOT, name)
      if (existsSync(source)) await cp(source, join(stage, name))
    }

    const archiveName = `${base}${goos === 'windows' ? '.zip' : '.tar.gz'}`
    await archive(stage, join(outDir, archiveName))
    names.push(archiveName)
  }
  await rm(stageRoot, { recursive: true, force: true })

  // Checksums let a downloader verify an artifact, and make it obvious when two
  // builds of the same tag produced different bytes.
  const lines = []
  for (const name of names.sort()) {
    lines.push(`${await sha256(join(outDir, name))}  ${name}`)
  }
  await writeFile(join(outDir, 'SHA256SUMS'), `${lines.join('\n')}\n`)
  log(`wrote ${names.length} archives and SHA256SUMS to ${outDir}`)
}

/** Builds the host binary into bin/. Not trimmed, so it is faster to relink. */
async function binary() {
  const info = await buildInfo()
  const goos = env.GOOS || GOOS_BY_PLATFORM[hostPlatform] || hostPlatform
  const goarch = env.GOARCH || GOARCH_BY_ARCH[hostArch] || hostArch
  const outDir = join(ROOT, 'bin')
  await mkdir(outDir, { recursive: true })

  await compile({ goos, goarch, out: join(outDir, exeName(goos)), info, trim: false })
  log(`built bin/${exeName(goos)} (${info.version})`)
}

async function clean() {
  for (const target of [join(ROOT, 'bin'), join(ROOT, 'dist'), WEB_DIST]) {
    await rm(target, { recursive: true, force: true })
  }
  await mkdir(EMBED_DIR, { recursive: true })
  for (const entry of await readdir(EMBED_DIR)) {
    if (entry === KEEP_FILE) continue
    await rm(join(EMBED_DIR, entry), { recursive: true, force: true })
  }
  if (!existsSync(join(EMBED_DIR, KEEP_FILE))) {
    await writeFile(join(EMBED_DIR, KEEP_FILE), KEEP_CONTENTS)
  }
  log('removed bin/, dist/, web/dist and the embedded frontend build')
}

/** Prints what a build would produce, which is the first thing to check when
 *  the running binary does not behave like the source tree. */
async function status() {
  const info = await buildInfo()
  const { command } = packageManager()
  const rows = [
    ['module', MODULE],
    ['version', info.version],
    ['commit', info.commit.slice(0, 12)],
    ['web/dist', existsSync(join(WEB_DIST, 'index.html')) ? 'built' : 'not built'],
    [
      'internal/web/dist',
      existsSync(join(EMBED_DIR, 'index.html'))
        ? 'embedded — the Go binary serves the app'
        : 'placeholder only — the Go binary serves a "not built" page',
    ],
    [KEEP_FILE, existsSync(join(EMBED_DIR, KEEP_FILE)) ? 'present' : 'MISSING (go build will fail)'],
    ['package manager', command],
    [
      'release targets',
      TARGETS.map((t) => `${t.goos}/${t.goarch}${t.goarm === undefined ? '' : `/v${t.goarm}`}`).join(', '),
    ],
  ]
  const width = Math.max(...rows.map(([name]) => name.length))
  process.stdout.write(`${rows.map(([name, value]) => `${name.padEnd(width)}  ${value}`).join('\n')}\n`)
}

const COMMANDS = {
  build: async () => {
    await buildWeb()
    await binary()
  },
  web: buildWeb,
  binary,
  release,
  clean,
  status,
}

const command = process.argv[2] ?? 'build'
const action = COMMANDS[command]
if (action === undefined) {
  process.stderr.write(
    `unknown command "${command}"\n\nusage: node scripts/build.mjs [${Object.keys(COMMANDS).join('|')}]\n`,
  )
  exit(2)
}

try {
  await action()
} catch (error) {
  fail(error.message)
}
