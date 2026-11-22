import { useMemo, useState } from 'react'
import { api } from '../api/client'
import type { InkColor, Project, ProjectInput, ProjectMode, ScheduleType } from '../api/types'
import { useApp } from '../hooks/useApp'
import { useSessionHeartbeat } from '../hooks/useHeartbeat'
import { navigate } from '../hooks/useNow'
import { weekdayName } from '../lib/date'
import { describeError, fieldErrorsFrom } from '../lib/errors'
import { pickUnusedColor } from '../lib/palette'
import { AppearanceField } from './AppearancePicker'
import { AlertIcon, ChevronLeftIcon } from './icons'
import { Button, Chip, Field, IconButton, Input, Segmented, Switch, Textarea } from './ui'

const SCHEDULE_OPTIONS: { value: ScheduleType; label: string }[] = [
  { value: 'daily', label: '每天' },
  { value: 'weekdays', label: '每周几' },
  { value: 'weekly', label: '每周' },
  { value: 'monthly', label: '每月' },
  { value: 'interval_days', label: '每N天' },
  { value: 'interval_hours', label: '每N小时' },
]

const UNIT_SUGGESTIONS = ['次', '杯', '分钟', '公里', '个', '页', '小时']

const MODE_OPTIONS: { value: ProjectMode; label: string }[] = [
  { value: 'check', label: '打勾一次' },
  { value: 'value', label: '累计数量' },
]

/**
 * Create / edit a project. The schedule picker is the heart: six families,
 * each revealing exactly the fields it needs. Client validation mirrors the
 * server's rules with Chinese messages; server field errors still land under
 * the right inputs, so the two ends cannot drift into contradicting the user.
 */
export function ProjectEditor({ projectId }: { projectId: string }) {
  const { projects, categories, today, isAdmin, refresh, toast } = useApp()
  const existing: Project | undefined = projectId === 'new' ? undefined : projects.find((p) => p.id === projectId)

  // New projects get a fresh ink whenever possible; an edit keeps its color.
  // Categories participate too, so a project does not disappear into the
  // category's own color in the charts.
  const usedColors = useMemo(
    () => [
      ...projects.filter((project) => project.id !== existing?.id).map((project) => project.color),
      ...categories.map((category) => category.color),
    ],
    [projects, categories, existing?.id],
  )

  const [name, setName] = useState(existing?.name ?? '')
  const [description, setDescription] = useState(existing?.description ?? '')
  const [icon, setIcon] = useState(existing?.icon ?? '✅')
  const [color, setColor] = useState<InkColor>(
    existing?.color ?? pickUnusedColor(usedColors),
  )
  const [categoryId, setCategoryId] = useState(existing?.categoryId ?? '')
  const [isPrivate, setIsPrivate] = useState(existing?.private ?? false)
  const [schedule, setSchedule] = useState<ScheduleType>(existing?.schedule ?? 'daily')
  const [mode, setMode] = useState<ProjectMode>(existing?.mode ?? 'check')
  const [prerequisites, setPrerequisites] = useState<string[]>(existing?.prerequisites ?? [])
  const [target, setTarget] = useState(existing !== undefined && existing.target > 0 ? String(existing.target) : '1')
  const [unit, setUnit] = useState(existing?.unit ?? '次')
  const [weekdays, setWeekdays] = useState<number[]>(existing?.weekdays ?? [1, 3, 5])
  const [interval, setInterval] = useState(existing !== undefined && existing.interval > 0 ? String(existing.interval) : '2')
  // Default to the server's day, not the device clock: the two can differ
  // around midnight/timezones, and a start date past the server's today would
  // make the native date input invalid and silently block submit.
  const [startDate, setStartDate] = useState(existing?.startDate ?? today)
  const [archived, setArchived] = useState(existing?.archived ?? false)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)

  // An open editor is "正在编辑": keep the session slid forward even while
  // the user types without saving.
  useSessionHeartbeat(isAdmin)

  if (!isAdmin) {
    return (
      <div className="view">
        <EditorBack />
        <p className="sheet-note">需要管理员登录才能创建或编辑项目。</p>
        <Button onClick={() => navigate('#login')}>登录</Button>
      </div>
    )
  }

  if (projectId !== 'new' && existing === undefined) {
    return (
      <div className="view">
        <EditorBack />
        <p className="sheet-note">项目不存在或已被删除。</p>
      </div>
    )
  }

  const isIntervalHours = schedule === 'interval_hours'
  const isIntervalDays = schedule === 'interval_days'
  const needsTarget = !isIntervalHours && mode === 'value'
  const needsWeekdays = schedule === 'weekdays'

  const validate = (): Record<string, string> => {
    const problems: Record<string, string> = {}
    if (name.trim() === '') problems['name'] = '请填写项目名称'
    else if ([...name.trim()].length > 40) problems['name'] = '名称最多 40 个字符'
    if ([...description].length > 500) problems['description'] = '说明最多 500 个字符'
    if (needsTarget) {
      const t = Number(target)
      if (!Number.isFinite(t) || t <= 0) problems['target'] = '目标需大于 0'
    }
    if (needsWeekdays && weekdays.length === 0) problems['weekdays'] = '至少选择一天'
    if (isIntervalHours || isIntervalDays) {
      const n = Number(interval)
      if (!Number.isFinite(n) || n < 1) problems['interval'] = '间隔至少为 1'
      else if (isIntervalHours && n > 720) problems['interval'] = '小时间隔最多 720'
      else if (isIntervalDays && (n > 365 || !Number.isInteger(n))) problems['interval'] = '天数需为 1–365 的整数'
    }
    if (!/^\d{4}-\d{2}-\d{2}$/.test(startDate)) problems['startDate'] = '请选择开始日期'
    else if (startDate > today) problems['startDate'] = '开始日期不能晚于今天'
    return problems
  }

  const submit = async () => {
    const problems = validate()
    setErrors(problems)
    if (Object.keys(problems).length > 0) return
    setBusy(true)
    const payload: ProjectInput = {
      name: name.trim(),
      description: description.trim(),
      icon,
      color,
      categoryId,
      private: isPrivate,
      schedule,
      mode,
      target: needsTarget ? Number(target) : 1,
      unit: unit.trim() === '' ? '次' : unit.trim(),
      weekdays: needsWeekdays ? weekdays : [],
      interval: isIntervalHours || isIntervalDays ? Number(interval) : 0,
      startDate,
      prerequisites,
    }
    try {
      if (existing === undefined) {
        await api.createProject(payload)
        toast(`已创建「${payload.name}」`, 'ok')
      } else {
        await api.updateProject(
          existing.id,
          { ...payload, archived },
          `"v${existing.version}"`,
        )
        toast('项目已更新', 'ok')
      }
      await refresh()
      window.history.back()
    } catch (error) {
      const fields = fieldErrorsFrom(error)
      setErrors(Object.keys(fields).length > 0 ? fields : { name: describeError(error) })
      setBusy(false)
    }
  }

  const toggleWeekday = (day: number) => {
    setWeekdays((current) =>
      current.includes(day) ? current.filter((d) => d !== day) : [...current, day].toSorted((a, b) => a - b),
    )
  }

  return (
    <div className="view editor" data-ink={color}>
      <EditorBack />
      <h1 className="view-title">{existing === undefined ? '新建项目' : '编辑项目'}</h1>

      {/* Native constraint validation would block submit before our Chinese
          field messages can render; validate() is the single reporting path. */}
      <form
        className="editor-form"
        noValidate
        onSubmit={(event) => {
          event.preventDefault()
          void submit()
        }}
      >
        <Field label="名称" htmlFor="p-name" error={errors['name']}>
          <Input id="p-name" value={name} maxLength={40} placeholder="例如：喝水" onChange={(event) => setName(event.target.value)} />
        </Field>

        <AppearanceField
          icon={icon}
          color={color}
          usedColors={usedColors}
          onIconChange={setIcon}
          onColorChange={setColor}
        />

        <Field label="说明（可选）" htmlFor="p-desc" error={errors['description']}>
          <Textarea
            id="p-desc"
            value={description}
            maxLength={500}
            rows={2}
            placeholder="这个项目要做什么、怎么做…"
            onChange={(event) => setDescription(event.target.value)}
          />
        </Field>

        <Field label="分类" htmlFor="p-category">
          <select id="p-category" className="input" value={categoryId} onChange={(event) => setCategoryId(event.target.value)}>
            <option value="">未分类</option>
            {categories.map((c) => (
              <option key={c.id} value={c.id}>
                {c.icon} {c.name}
              </option>
            ))}
          </select>
        </Field>

        <Field label="打卡频率">
          <Segmented options={SCHEDULE_OPTIONS} value={schedule} onChange={setSchedule} ariaLabel="打卡频率" />
        </Field>

        <Field label="完成方式" hint={mode === 'check' ? '每天打一次勾即完成，「30 分钟」等写在说明里' : '累计数量达到目标才算完成'}>
          <Segmented options={MODE_OPTIONS} value={mode} onChange={setMode} ariaLabel="完成方式" />
        </Field>

        <Field label="前置条件（可选）" error={errors['prerequisites']} hint="选了的项目必须当天先完成，本项目才解锁；可多选、可链式，服务器会拒绝成环">
          <div className="chip-row prereq-picker" role="group" aria-label="前置条件">
            {projects
              .filter((p) => p.id !== projectId)
              .map((p) => (
                <Chip
                  key={p.id}
                  active={prerequisites.includes(p.id)}
                  onClick={() =>
                    setPrerequisites((cur) =>
                      cur.includes(p.id) ? cur.filter((x) => x !== p.id) : [...cur, p.id],
                    )
                  }
                >
                  <span aria-hidden="true">{p.icon}</span> {p.name}
                </Chip>
              ))}
          </div>
        </Field>

        {needsWeekdays ? (
          <Field label="选择星期" error={errors['weekdays']}>
            <div className="weekday-picker" role="group" aria-label="选择星期">
              {[1, 2, 3, 4, 5, 6, 7].map((day) => (
                <button
                  key={day}
                  type="button"
                  className="weekday-option"
                  data-active={weekdays.includes(day) ? 'true' : undefined}
                  aria-pressed={weekdays.includes(day)}
                  onClick={() => toggleWeekday(day)}
                >
                  {weekdayName(day)}
                </button>
              ))}
            </div>
          </Field>
        ) : null}

        {needsTarget ? (
          <div className="field-row">
            <Field label={`每${schedule === 'daily' || schedule === 'weekdays' ? '天' : schedule === 'weekly' ? '周' : schedule === 'monthly' ? '月' : `${interval || 'N'} 天`}目标`} htmlFor="p-target" error={errors['target']}>
              <Input id="p-target" type="number" inputMode="decimal" min="0.1" step="any" value={target} onChange={(event) => setTarget(event.target.value)} />
            </Field>
            <Field label="单位" htmlFor="p-unit" error={errors['unit']}>
              <Input id="p-unit" value={unit} maxLength={8} list="unit-suggestions" onChange={(event) => setUnit(event.target.value)} />
              <datalist id="unit-suggestions">
                {UNIT_SUGGESTIONS.map((u) => (
                  <option key={u} value={u}>
                    {u}
                  </option>
                ))}
              </datalist>
            </Field>
          </div>
        ) : null}

        {isIntervalHours || isIntervalDays ? (
          <Field label={isIntervalHours ? '间隔（小时）' : '间隔（天）'} htmlFor="p-interval" error={errors['interval']} hint={isIntervalHours ? '例如 2：每 2 小时可打卡一次，打卡后重新倒计时' : '例如 3：每 3 天为一个周期'}>
            <Input id="p-interval" type="number" inputMode="numeric" min="1" step={isIntervalDays ? 1 : 0.5} value={interval} onChange={(event) => setInterval(event.target.value)} />
          </Field>
        ) : null}

        <Field label="开始日期" htmlFor="p-start" error={errors['startDate']} hint="统计与连续记录从这一天算起">
          <Input id="p-start" type="date" value={startDate} max={today} min="2000-01-01" onChange={(event) => setStartDate(event.target.value)} />
        </Field>

        <Switch
          checked={isPrivate}
          onChange={setIsPrivate}
          label="仅登录可见"
          hint="匿名访客看不到这个项目"
        />
        {existing !== undefined ? (
          <Switch checked={archived} onChange={setArchived} label="归档" hint="归档后不再出现在今日页，历史保留" />
        ) : null}

        {Object.keys(errors).length > 0 ? (
          <p className="form-error" role="alert">
            <AlertIcon size={13} /> 请检查标红的字段。
          </p>
        ) : null}
        <div className="form-actions">
          <Button variant="ghost" onClick={() => window.history.back()}>
            取消
          </Button>
          <Button type="submit" pending={busy}>
            {existing === undefined ? '创建项目' : '保存修改'}
          </Button>
        </div>
      </form>
    </div>
  )
}

function EditorBack() {
  return (
    <div className="detail-back">
      <IconButton label="返回" onClick={() => window.history.back()}>
        <ChevronLeftIcon />
      </IconButton>
    </div>
  )
}
