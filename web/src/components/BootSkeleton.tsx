import { SealMark } from './icons'

/**
 * Shown while the bootstrap requests are in flight: the app's own shape in
 * quiet outline, so the first paint already tells the user what is coming.
 */
export function BootSkeleton() {
  return (
    <div className="boot" role="status" aria-label="正在连接服务器">
      <div className="boot-bar">
        <SealMark size={24} className="boot-seal" />
        <span className="wordmark">水花打卡</span>
      </div>
      <div className="boot-body">
        <span className="skeleton skeleton-head" />
        <span className="skeleton skeleton-card" />
        <span className="skeleton skeleton-card" />
        <span className="skeleton skeleton-card" />
      </div>
      <p className="boot-message">正在连接服务器…</p>
    </div>
  )
}