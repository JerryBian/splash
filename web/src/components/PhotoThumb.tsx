import { api } from '../api/client'

/**
 * A lazy thumbnail for a check-in's proof photo.
 *
 * The image is fetched by URL, never inlined in the record payload: history
 * pages can hold dozens of records, and a photo is only decoded when the
 * browser actually paints the row. Clicking opens the full image in a new tab
 * (same-origin, so the session cookie rides along).
 */
export function PhotoThumb({ checkinId }: { checkinId: string }) {
  const url = api.checkinPhotoUrl(checkinId)
  return (
    <a className="record-thumb" href={url} target="_blank" rel="noreferrer" title="查看打卡照片">
      <img src={url} alt="打卡照片" loading="lazy" />
    </a>
  )
}
