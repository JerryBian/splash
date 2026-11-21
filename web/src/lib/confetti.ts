/**
 * The ink splatter: a short canvas burst in the project's ink color, fired
 * from the stamp button when a check-in lands. It answers the user's action —
 * the one orchestrated motion moment in the app — and respects
 * prefers-reduced-motion by not firing at all.
 *
 * One shared canvas is created lazily and removed when the last particle
 * dies, so idle pages carry zero overhead.
 */

type Particle = {
  x: number
  y: number
  vx: number
  vy: number
  r: number
  life: number
  decay: number
  color: string
}

let canvas: HTMLCanvasElement | null = null
let ctx: CanvasRenderingContext2D | null = null
let particles: Particle[] = []
let raf = 0
let last = 0

function ensureCanvas(): CanvasRenderingContext2D | null {
  if (ctx !== null) return ctx
  canvas = document.createElement('canvas')
  canvas.setAttribute('aria-hidden', 'true')
  canvas.style.cssText =
    'position:fixed;inset:0;width:100vw;height:100vh;pointer-events:none;z-index:90'
  document.body.appendChild(canvas)
  ctx = canvas.getContext('2d')
  resize()
  window.addEventListener('resize', resize)
  return ctx
}

function resize(): void {
  if (canvas === null) return
  const dpr = Math.min(window.devicePixelRatio || 1, 2)
  canvas.width = Math.floor(window.innerWidth * dpr)
  canvas.height = Math.floor(window.innerHeight * dpr)
  ctx?.setTransform(dpr, 0, 0, dpr, 0, 0)
}

function teardown(): void {
  cancelAnimationFrame(raf)
  raf = 0
  window.removeEventListener('resize', resize)
  canvas?.remove()
  canvas = null
  ctx = null
  particles = []
}

function frame(now: number): void {
  if (ctx === null || canvas === null) return
  const dt = Math.min(48, now - last) / 16.67
  last = now
  ctx.clearRect(0, 0, window.innerWidth, window.innerHeight)
  particles = particles.filter((p) => p.life > 0)
  for (const p of particles) {
    p.x += p.vx * dt
    p.y += p.vy * dt
    p.vy += 0.22 * dt // gravity
    p.vx *= 0.985
    p.life -= p.decay * dt
    ctx.globalAlpha = Math.max(0, Math.min(1, p.life))
    ctx.fillStyle = p.color
    ctx.beginPath()
    ctx.arc(p.x, p.y, p.r, 0, Math.PI * 2)
    ctx.fill()
  }
  ctx.globalAlpha = 1
  if (particles.length === 0) {
    teardown()
    return
  }
  raf = requestAnimationFrame(frame)
}

/**
 * Fires a splatter from (x, y) in viewport coordinates. color is any CSS
 * color — pass the computed ink so light/dark themes splatter correctly.
 */
export function splatter(x: number, y: number, color: string): void {
  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return
  const context = ensureCanvas()
  if (context === null) return
  const count = 26
  for (let i = 0; i < count; i++) {
    const angle = (Math.PI * 2 * i) / count + Math.random() * 0.5
    const speed = 2.2 + Math.random() * 4.2
    particles.push({
      x,
      y,
      vx: Math.cos(angle) * speed,
      vy: Math.sin(angle) * speed - 1.6,
      r: 1.6 + Math.random() * 3,
      life: 1,
      decay: 0.018 + Math.random() * 0.02,
      color,
    })
  }
  if (raf === 0) {
    last = performance.now()
    raf = requestAnimationFrame(frame)
  }
}

/** Reads the resolved ink color of an element (custom property --ink). */
export function inkColorOf(el: HTMLElement | null): string {
  if (el === null) return '#d93025'
  const value = getComputedStyle(el).getPropertyValue('--ink').trim()
  return value === '' ? '#d93025' : value
}
