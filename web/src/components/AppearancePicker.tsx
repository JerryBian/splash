import { useMemo, useState } from 'react'
import { INK_COLORS, type InkColor } from '../api/types'
import { pickUnusedColor } from '../lib/palette'
import { INK_NAMES } from '../lib/schedule'
import { ChevronRightIcon, RefreshIcon } from './icons'
import { Sheet } from './overlays'
import { Button, Chip, ColorPicker } from './ui'

/**
 * A compact appearance control: one row in the form, a grouped picker in a
 * sheet. The old editor rendered every icon as a wall of tiles; this keeps the
 * form short while making a much larger catalogue navigable.
 */

type IconGroup = {
  key: string
  label: string
  icon: string
  choices: readonly string[]
}

const COMMON_ICONS = [
  '✅', '💧', '🏃', '🧘', '🏋️', '📚', '💻', '📝',
  '🥗', '💰', '🏠', '🌙', '☕', '🔥', '🎯', '🏆',
  '💪', '🚶', '🚴', '🧹', '🐕', '❤️', '⭐', '🎨',
] as const

const HEALTH_ICONS = [
  '🏊', '⚽', '🏀', '🎾', '🏓', '🏸', '🥊', '🥋',
  '🧗', '🤸', '🚣', '⛹️', '🩺', '💊', '🦷', '🥦',
  '🍎', '🥕', '🍵', '🥛', '🍳', '🥑', '🍇', '🍊',
] as const

const STUDY_ICONS = [
  '🧠', '🎧', '🎹', '🗣️', '📖', '🔬', '🧮', '📓',
  '📔', '📒', '📕', '📗', '📘', '📙', '📰', '🖊️',
  '📐', '📏', '🎓', '📊', '📈', '🗂️', '📅', '💡',
] as const

const LIFE_ICONS = [
  '🏡', '🛋️', '🛏️', '🚿', '🛁', '🧺', '🧴', '🧼',
  '🪥', '🚽', '🪴', '🌱', '🌿', '🍀', '🌵', '🌸',
  '🌻', '🐈', '🐟', '🐦', '🔑', '🛒', '👕', '👟',
] as const

const MOOD_ICONS = [
  '😄', '🙂', '😐', '😪', '💖', '🙏', '🌅', '😴',
  '☀️', '🌈', '✨', '🥇', '🎉', '🧸', '🫶', '🕯️',
  '🎵', '🎸', '🎬', '📷', '🧩', '🚀', '🗺️', '🧭',
] as const

const OTHER_ICONS = [
  '📱', '📵', '🎮', '🎲', '🪄', '🛸', '⏰', '⏳',
  '🔋', '🔒', '🔍', '🧲', '🛠️', '🔧', '🪛', '🖥️',
  '🖨️', '⌨️', '🖱️', '📦', '🗑️', '🧾', '💳', '🪙',
] as const

const ICON_GROUPS: readonly IconGroup[] = [
  { key: 'common', label: '常用', icon: '✨', choices: COMMON_ICONS },
  { key: 'health', label: '健康', icon: '💪', choices: HEALTH_ICONS },
  { key: 'study', label: '学习', icon: '📚', choices: STUDY_ICONS },
  { key: 'life', label: '生活', icon: '🏡', choices: LIFE_ICONS },
  { key: 'mood', label: '心情', icon: '🌤️', choices: MOOD_ICONS },
  { key: 'other', label: '其他', icon: '🧩', choices: OTHER_ICONS },
]

const RECENT_KEY = 'splash-recent-icons'
const RECENT_LIMIT = 12
const ICON_COUNT = new Set(ICON_GROUPS.flatMap((group) => group.choices)).size
const NO_USED_COLORS: readonly string[] = []

/** Recent icons are a convenience, never a requirement: storage can throw. */
function readRecentIcons(): string[] {
  try {
    const raw = localStorage.getItem(RECENT_KEY)
    if (raw === null) return []
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.filter((value): value is string => typeof value === 'string').slice(0, RECENT_LIMIT)
  } catch {
    return []
  }
}

function rememberIcon(current: readonly string[], icon: string): string[] {
  const next = [icon, ...current.filter((value) => value !== icon)].slice(0, RECENT_LIMIT)
  try {
    localStorage.setItem(RECENT_KEY, JSON.stringify(next))
  } catch {
    // The current picker still remembers it; only persistence is lost.
  }
  return next
}

export function AppearanceField({
  icon,
  color,
  usedColors = NO_USED_COLORS,
  onIconChange,
  onColorChange,
}: {
  icon: string
  color: InkColor
  /** Colors of the other projects/categories, for the random button. */
  usedColors?: readonly string[]
  onIconChange: (icon: string) => void
  onColorChange: (color: InkColor) => void
}) {
  const [open, setOpen] = useState(false)

  return (
    <>
      <button
        type="button"
        className="appearance-trigger"
        data-ink={color}
        aria-haspopup="dialog"
        aria-label={`图标与颜色：${INK_NAMES[color] ?? color}`}
        onClick={() => setOpen(true)}
      >
        <span className="appearance-trigger-icon" aria-hidden="true">
          {icon}
        </span>
        <span className="appearance-trigger-text">
          <span className="appearance-trigger-label">图标与颜色</span>
          <span className="appearance-trigger-value">{INK_NAMES[color] ?? color} · 点击更换</span>
        </span>
        <ChevronRightIcon size={17} />
      </button>
      {open ? (
        <AppearanceSheet
          icon={icon}
          color={color}
          usedColors={usedColors}
          onIconChange={onIconChange}
          onColorChange={onColorChange}
          onClose={() => setOpen(false)}
        />
      ) : null}
    </>
  )
}

function AppearanceSheet({
  icon,
  color,
  usedColors,
  onIconChange,
  onColorChange,
  onClose,
}: {
  icon: string
  color: InkColor
  usedColors: readonly string[]
  onIconChange: (icon: string) => void
  onColorChange: (color: InkColor) => void
  onClose: () => void
}) {
  const [recent, setRecent] = useState<string[]>(readRecentIcons)
  const groups = useMemo<IconGroup[]>(
    () =>
      recent.length > 0
        ? [{ key: 'recent', label: '最近', icon: '🕘', choices: recent }, ...ICON_GROUPS]
        : [...ICON_GROUPS],
    [recent],
  )
  const [groupId, setGroupId] = useState<string>(() => {
    const first = groups.find((group) => group.choices.includes(icon))
    return first?.key ?? 'common'
  })
  const activeGroup = groups.find((group) => group.key === groupId) ?? groups[0]

  const chooseIcon = (next: string) => {
    onIconChange(next)
    setRecent(rememberIcon(recent, next))
  }

  return (
    <Sheet
      open
      onClose={onClose}
      title="图标与颜色"
      footer={
        <Button onClick={onClose}>
          完成
        </Button>
      }
    >
      <div className="appearance-hero">
        <span className="appearance-hero-icon" data-ink={color} aria-hidden="true">
          {icon}
        </span>
        <div className="appearance-hero-text">
          <strong>{INK_NAMES[color] ?? color}</strong>
          <span>颜色同时用于卡片边条、图表和印章</span>
        </div>
        <span className="appearance-count">{INK_COLORS.length} 色</span>
      </div>

      <section className="appearance-section" aria-label="颜色">
        <div className="appearance-section-head">
          <h3 className="sheet-subhead">颜色</h3>
          <div className="appearance-section-actions">
            <span className="appearance-section-note">{INK_NAMES[color] ?? color}</span>
            <button
              type="button"
              className="appearance-random"
              onClick={() => onColorChange(pickUnusedColor([...usedColors, color]))}
            >
              <RefreshIcon size={12} /> 随机
            </button>
          </div>
        </div>
        <ColorPicker value={color} onChange={onColorChange} />
      </section>

      <section className="appearance-section" aria-label="图标">
        <div className="appearance-section-head">
          <h3 className="sheet-subhead">图标</h3>
          <span className="appearance-section-note">选择一种心情或场景</span>
        </div>
        <div className="chip-row appearance-groups" role="group" aria-label="图标分类">
          {groups.map((group) => (
            <Chip key={group.key} active={group.key === groupId} onClick={() => setGroupId(group.key)}>
              <span aria-hidden="true">{group.icon}</span> {group.label}
            </Chip>
          ))}
        </div>
        <div className="emoji-picker appearance-icons" role="group" aria-label="图标">
          {activeGroup?.choices.map((emoji) => (
            <button
              key={emoji}
              type="button"
              className="emoji-option"
              data-active={emoji === icon ? 'true' : undefined}
              aria-label={`图标 ${emoji}`}
              aria-pressed={emoji === icon}
              onClick={() => chooseIcon(emoji)}
            >
              {emoji}
            </button>
          ))}
        </div>
        {/* A custom emoji still works even if it is not in the curated set. */}
        <p className="sheet-note appearance-note">
          共 {ICON_COUNT} 个图标，按分类浏览；已有项目的自定义图标会保留。
        </p>
      </section>
    </Sheet>
  )
}
