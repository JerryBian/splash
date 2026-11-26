import { describe, expect, it } from 'vitest'
import { fitWithin, isSupportedImageType, readPhotoFile } from './photo'

describe('photo intake', () => {
  it('accepts only the raster types the server can serve', () => {
    expect(isSupportedImageType('image/png')).toBe(true)
    expect(isSupportedImageType('image/jpeg')).toBe(true)
    expect(isSupportedImageType('image/webp')).toBe(true)
    expect(isSupportedImageType('image/svg+xml')).toBe(false)
    expect(isSupportedImageType('text/html')).toBe(false)
    expect(isSupportedImageType('')).toBe(false)
  })

  it('fits a large picture inside the long-edge cap', () => {
    expect(fitWithin(2048, 1024, 1024)).toEqual({ width: 1024, height: 512 })
    expect(fitWithin(600, 1200, 1024)).toEqual({ width: 512, height: 1024 })
    expect(fitWithin(800, 600, 1024)).toEqual({ width: 800, height: 600 })
    // Degenerate dimensions never produce a zero-sized canvas.
    expect(fitWithin(0, 0, 1024)).toEqual({ width: 1, height: 1 })
  })

  it('reads a small file straight through as a data URL', async () => {
    const file = new File([new Uint8Array([0x89, 0x50, 0x4e, 0x47])], 'proof.png', { type: 'image/png' })
    const result = await readPhotoFile(file)
    if (!result.ok) throw new Error(result.error)
    expect(result.dataUrl.startsWith('data:image/png;base64,')).toBe(true)
  })

  it('rejects an unsupported or empty file with a readable message', async () => {
    const svg = new File(['<svg/>'], 'x.svg', { type: 'image/svg+xml' })
    const badType = await readPhotoFile(svg)
    if (badType.ok) throw new Error('an SVG must be rejected')
    expect(badType.error).toContain('JPEG')

    const empty = new File([], 'empty.png', { type: 'image/png' })
    const emptyResult = await readPhotoFile(empty)
    expect(emptyResult.ok).toBe(false)
  })
})
