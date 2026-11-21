import { Suspense, useState, useTransition } from 'react'
import { api } from '../api/client'
import type { Project } from '../api/types'
import { useApp } from '../hooks/useApp'
import { useSessionHeartbeat } from '../hooks/useHeartbeat'
import { useLoaded } from '../hooks/useLoaded'
import { fetchDay, invalidate } from '../lib/async'
import { formatClock, localToday } from '../lib/date'
import { describeError } from '../lib/errors'
import { formatValue, progressLabel } from '../lib/format'
import { CheckIcon, MinusIcon, PlusIcon, SkipIcon, TrashIcon } from './icons'
import { ConfirmDialog, Sheet } from './overlays'
import { Button, Field, IconButton, Input, RecordBadges } from './ui'

const MOODS = ['😄', '🙂', '😐', '😪', '🔥'] as const

/**
 * The record sheet: how much, a note, a mood, and which day. It doubles as
 * the day's record list (undo lives here), the backfill (补卡) entry point,
 * and the skip toggle — everything a check-in day needs in one place.
 *
 * Day data loads with use() + Suspense through the shared cache, so the
 * detail page's day panel and this sheet never fetch the same day twice.
 */
export function CheckinSheet({
  project,
  initialDate,
  onClose,
}: {
  project: Project
  initialDate: string
  onClose: () => void
}) {
  const { today, refresh, toast } = useApp()
  const [date, setDate] = useState(initialDate)
  const [value, setValue] = useState('1')
  const [note, setNote] = useState('')
  const [mood, setMood] = useState<string>('')
  const [busy, setBusy] = useState(false)
  const [version, setVersion] = useState(0)
  const [, startTransition] = useTransition()

  useSessionHeartbeat(true)

  const isInterval = project.schedule === 'interval_hours'
  const isCheck = project.mode === 'check'
  const status = project.status
  const [confirmGate, setConfirmGate] = useState(false)

  const reloadDay = () => {
    invalidate(`day:${project.id}:`)
    startTransition(() => setVersion((v) => v + 1))
  }

  const numericValue = Number(value)
  const valueValid = Number.isFinite(numericValue) && numericValue > 0
  const gateUnmet = date === today && status !== undefined && status.unmet.length > 0

  const submit = async () => {
    if ((!isCheck && !isInterval && !valueValid) || busy) return
    // Checking while a prerequisite is unmet is allowed but flagged; make the
    // user acknowledge it once so the violation is intentional, not accidental.
    if (gateUnmet && !confirmGate) {
      setConfirmGate(true)
      return
    }
    setConfirmGate(false)
    setBusy(true)
    try {
      const created = await api.createCheckin(project.id, {
        value: isInterval || isCheck ? 1 : numericValue,
        note: note.trim(),
        mood,
        date,
        tzOffset: -new Date().getTimezoneOffset(),
      })
      toast(
        created.violated ? `已打卡「${project.name}」，但记了一次前置违规` : `已为「${project.name}」打卡`,
        created.violated ? 'error' : 'ok',
      )
      setNote('')
      setMood('')
      setValue('1')
      await refresh()
      reloadDay()
    } catch (error) {
      toast(describeError(error), 'error')
    } finally {
      setBusy(false)
    }
  }

  const remaining =
    status !== undefined && status.target > 0 ? Math.max(0, status.target - status.value) : 0

  return (
    <>
    <Sheet
      open
      onClose={onClose}
      title={
        <span className="sheet-project">
          <span className="sheet-icon" data-ink={project.color} aria-hidden="true">
            {project.icon}
          </span>
          {project.name}
        </span>
      }
      footer={
        <Button
          pending={busy}
          disabled={!isInterval && !isCheck && !valueValid}
          onClick={() => void submit()}
        >
          <CheckIcon size={16} strokeWidth={2.4} />
          {isInterval || isCheck ? '打卡' : `记录 ${valueValid ? formatValue(numericValue) : '—'} ${project.unit}`}
        </Button>
      }
    >
      <div className="sheet-section">
        <Field label="日期" htmlFor="checkin-date" hint={date !== today ? '补卡：记录过去的某一天' : undefined}>
          <Input
            id="checkin-date"
            type="date"
            value={date}
            min={project.startDate}
            max={today}
            onChange={(event) => setDate(event.target.value || localToday())}
          />
        </Field>

        {isCheck ? (
          <p className="sheet-note">打勾型项目：每天打一次勾即完成，无需填数量。</p>
        ) : null}

        {!isInterval && !isCheck ? (
          <Field label={`数量（${project.unit}）`} htmlFor="checkin-value">
            <div className="stepper">
              <IconButton
                label="减少"
                onClick={() => setValue(String(Math.max(0.5, (Number(value) || 1) - 1)))}
              >
                <MinusIcon size={16} />
              </IconButton>
              <Input
                id="checkin-value"
                type="number"
                inputMode="decimal"
                min="0.1"
                step="any"
                value={value}
                onChange={(event) => setValue(event.target.value)}
              />
              <IconButton label="增加" onClick={() => setValue(String((Number(value) || 0) + 1))}>
                <PlusIcon size={16} />
              </IconButton>
            </div>
            {status !== undefined && status.target > 1 ? (
              <div className="quick-values">
                <button type="button" className="mini-chip" onClick={() => setValue('1')}>
                  +1
                </button>
                <button type="button" className="mini-chip" onClick={() => setValue('5')}>
                  +5
                </button>
                {remaining > 0 ? (
                  <button type="button" className="mini-chip" onClick={() => setValue(String(remaining))}>
                    补足 {formatValue(remaining)}
                  </button>
                ) : null}
                <span className="quick-hint">
                  本周期 {progressLabel(project, status.value, status.target)}
                </span>
              </div>
            ) : null}
          </Field>
        ) : (
          <p className="sheet-note">间隔项目每次记 1 次，打卡后重新开始倒计时。</p>
        )}

        <Field label="备注（可选）" htmlFor="checkin-note">
          <Input
            id="checkin-note"
            value={note}
            maxLength={200}
            placeholder="此刻的想法…"
            onChange={(event) => setNote(event.target.value)}
          />
        </Field>

        <div className="mood-row" role="group" aria-label="心情">
          {MOODS.map((m) => (
            <button
              key={m}
              type="button"
              className="mood"
              data-active={mood === m ? 'true' : undefined}
              aria-pressed={mood === m}
              aria-label={`心情 ${m}`}
              onClick={() => setMood(mood === m ? '' : m)}
            >
              {m}
            </button>
          ))}
        </div>
      </div>

      <div className="sheet-section">
        <Suspense fallback={<p className="sheet-note">正在读取当天记录…</p>}>
          <DayRecords
            project={project}
            day={date}
            version={version}
            onChanged={async () => {
              await refresh()
              reloadDay()
            }}
          />
        </Suspense>
      </div>
    </Sheet>
    <ConfirmDialog
      open={confirmGate}
      title="前置条件未满足"
      message={`今天还有 ${status?.unmet.length ?? 0} 个前置项目没完成。现在打卡会被标记为一次前置违规；之后补卡前置可自动消除。仍要打卡吗？`}
      confirmLabel="仍要打卡"
      onConfirm={() => void submit()}
      onClose={() => setConfirmGate(false)}
    />
    </>
  )
}

/** The selected day's records and skip toggle, loaded through the shared cache. */
function DayRecords({
  project,
  day,
  version,
  onChanged,
}: {
  project: Project
  day: string
  version: number
  onChanged: () => Promise<void>
}) {
  const { toast } = useApp()
  const result = useLoaded(`day:${project.id}:${day}:${version}`, () => fetchDay(project.id, day))
  const [busy, setBusy] = useState(false)

  if (!result.ok) {
    return <p className="field-error">{describeError(result.error)}</p>
  }
  const { records, skipped } = result.data

  const toggleSkip = async () => {
    setBusy(true)
    try {
      if (skipped) {
        await api.deleteSkip(project.id, day)
        toast('已取消跳过', 'info')
      } else {
        await api.createSkip(project.id, day)
        toast('这一天已标记为跳过，不影响连续记录', 'ok')
      }
      await onChanged()
    } catch (error) {
      toast(describeError(error), 'error')
    } finally {
      setBusy(false)
    }
  }

  const removeRecord = async (id: string) => {
    try {
      await api.deleteCheckin(id)
      toast('已撤销这条记录', 'info')
      await onChanged()
    } catch (error) {
      toast(describeError(error), 'error')
    }
  }

  return (
    <>
      <button
        type="button"
        className="skip-toggle"
        data-on={skipped ? 'true' : undefined}
        disabled={busy}
        onClick={() => void toggleSkip()}
      >
        <SkipIcon size={15} />
        {skipped ? '取消跳过这一天' : '跳过这一天（请假，不影响连续）'}
      </button>
      <h3 className="sheet-subhead sheet-subhead-gap">当天记录 · {records.length}</h3>
      {records.length === 0 ? (
        <p className="sheet-note">这一天还没有记录。</p>
      ) : (
        <ul className="record-list">
          {records.map((record) => (
            <li key={record.id} className="record">
              <span className="record-value">
                {formatValue(record.value)} {project.unit}
              </span>
              <span className="record-meta">
                {formatClock(record.checkedAt)}
                {record.mood !== undefined && record.mood !== '' ? ` · ${record.mood}` : ''}
                {record.note !== undefined && record.note !== '' ? ` · ${record.note}` : ''}
              </span>
              <RecordBadges backfilled={record.backfilled} violated={record.violated} />
              <IconButton label="撤销这条记录" onClick={() => void removeRecord(record.id)}>
                <TrashIcon size={15} />
              </IconButton>
            </li>
          ))}
        </ul>
      )}
    </>
  )
}
