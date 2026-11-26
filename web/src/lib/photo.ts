/**
 * Client-side photo intake for check-in proof pictures.
 *
 * The server stores one image per check-in as a data URL and caps the string at
 * 400 KB, so this module is the gatekeeper: accept only decodable raster types,
 * reuse a small file as-is, and downscale/recompress anything that would not
 * fit. That keeps phone cameras (several MB per shot) usable without shipping
 * megabytes over the wire or into the database.
 */

/** Mirrors store.MaxPhotoBytes: the longest data URL the server accepts. */
export const MAX_PHOTO_DATA_URL = 400_000

/** Refuse absurd inputs before decoding them into memory. */
const MAX_INPUT_BYTES = 12 * 1024 * 1024

/** Long-edge cap before compression; plenty for a proof-of-done picture. */
const MAX_EDGE = 1024

const SUPPORTED_TYPES = ['image/jpeg', 'image/png', 'image/webp'] as const

export type PhotoResult = { ok: true; dataUrl: string } | { ok: false; error: string }

/** True for the raster types the server's allowlist can serve back. */
export function isSupportedImageType(type: string): boolean {
  return (SUPPORTED_TYPES as readonly string[]).includes(type)
}

/** Scales width/height down to fit inside a square of `max`, preserving ratio. */
export function fitWithin(width: number, height: number, max: number): { width: number; height: number } {
  const longest = Math.max(width, height)
  if (longest <= max || longest <= 0) {
    return { width: Math.max(1, width), height: Math.max(1, height) }
  }
  const scale = max / longest
  return { width: Math.max(1, Math.round(width * scale)), height: Math.max(1, Math.round(height * scale)) }
}

/** Reads a picked file into a data URL the server accepts, downscaling if needed. */
export async function readPhotoFile(file: File): Promise<PhotoResult> {
  if (!isSupportedImageType(file.type)) {
    return { ok: false, error: '只支持 JPEG、PNG 或 WebP 图片' }
  }
  if (file.size === 0) {
    return { ok: false, error: '图片文件为空' }
  }
  if (file.size > MAX_INPUT_BYTES) {
    return { ok: false, error: '图片太大，请选择 12MB 以内的图片' }
  }
  const raw = await readDataURL(file)
  if (raw === null) {
    return { ok: false, error: '读取图片失败，请重试' }
  }
  // A small file is already under the cap; skipping the canvas keeps the
  // common case instant and works in browsers that refuse canvas on some
  // origins (and in jsdom, where 2D contexts do not exist).
  if (raw.length <= MAX_PHOTO_DATA_URL) {
    return { ok: true, dataUrl: raw }
  }
  const shrunk = await shrink(raw)
  if (shrunk === null) {
    return { ok: false, error: '图片压缩失败，请换一张或先在相册里裁剪' }
  }
  return { ok: true, dataUrl: shrunk }
}

function readDataURL(file: File): Promise<string | null> {
  return new Promise((resolve) => {
    const reader = new FileReader()
    reader.addEventListener(
      'load',
      () => resolve(typeof reader.result === 'string' ? reader.result : null),
      { once: true },
    )
    reader.addEventListener('error', () => resolve(null), { once: true })
    reader.readAsDataURL(file)
  })
}

function loadImage(source: string): Promise<HTMLImageElement | null> {
  return new Promise((resolve) => {
    const image = new Image()
    image.addEventListener('load', () => resolve(image), { once: true })
    image.addEventListener('error', () => resolve(null), { once: true })
    image.src = source
  })
}

async function shrink(dataUrl: string): Promise<string | null> {
  const image = await loadImage(dataUrl)
  if (image === null) return null
  const box = fitWithin(image.naturalWidth, image.naturalHeight, MAX_EDGE)
  const canvas = document.createElement('canvas')
  canvas.width = box.width
  canvas.height = box.height
  const context = canvas.getContext('2d')
  if (context === null) return null
  context.drawImage(image, 0, 0, box.width, box.height)
  // Try progressively harsher JPEG settings; the first that fits wins.
  for (const quality of [0.72, 0.55, 0.4]) {
    const encoded = canvas.toDataURL('image/jpeg', quality)
    if (encoded.length <= MAX_PHOTO_DATA_URL) return encoded
  }
  return null
}
