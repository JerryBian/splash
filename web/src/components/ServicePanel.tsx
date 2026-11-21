import type { ReactNode } from 'react'
import type { Health, VersionInfo } from '../api/types'
import { useLastTrace } from '../api/trace'
import { formatDuration } from '../lib/format'

/**
 * The facts about the running process, kept from the original skeleton: this
 * app is one Go binary with the frontend compiled in, and the diagnostics
 * panel proves it with real numbers — including the latest actual round trip
 * (method, status, latency, request id), nothing simulated.
 */
export function ServicePanel({
  health,
  version,
  projectCount,
}: {
  health: Health
  version: VersionInfo
  projectCount: number
}) {
  const trace = useLastTrace()
  return (
    <div className="service-panel">
      <dl className="facts">
        <Fact term="状态">
          <span className="chip-status" data-ok={health.status === 'ok' ? 'true' : undefined}>
            <span className="chip-dot" aria-hidden="true" />
            {health.status === 'ok' ? '正常' : health.status}
          </span>
        </Fact>
        <Fact term="环境">{version.environment}</Fact>
        <Fact term="版本">{version.version}</Fact>
        <Fact term="提交" title={version.commit}>
          {version.commit}
        </Fact>
        <Fact term="工具链">{version.goVersion}</Fact>
        <Fact term="平台">{version.platform}</Fact>
        <Fact term="运行时长">{formatDuration(health.uptimeSeconds)}</Fact>
        <Fact term="协程数">{health.goroutines}</Fact>
        <Fact term="项目数">{projectCount}</Fact>
      </dl>

      <h4 className="facts-head">最近一次请求</h4>
      {trace.state === 'idle' ? (
        <p className="sheet-note">还没有请求。</p>
      ) : (
        <dl className="facts">
          <Fact term="请求">
            {trace.method} {trace.path}
          </Fact>
          <Fact term="状态">{trace.status ?? '网络失败'}</Fact>
          <Fact term="耗时">{trace.durationMs ?? 0} ms</Fact>
          <Fact term="请求 ID" title={trace.requestId ?? undefined}>
            {trace.requestId ?? '—'}
          </Fact>
        </dl>
      )}
      <p className="panel-note">
        数据保存在服务器端的 SQLite 中（路径由 SPLASH_DB_PATH 控制），前端编译进同一个二进制。
      </p>
    </div>
  )
}

function Fact({ term, children, title }: { term: string; children: ReactNode; title?: string }) {
  return (
    <div className="fact">
      <dt className="fact-term">{term}</dt>
      <dd className="fact-value" title={title}>
        {children}
      </dd>
    </div>
  )
}
