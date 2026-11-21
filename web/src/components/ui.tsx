import type { ComponentProps, ReactNode } from 'react'
import { INK_COLORS, type InkColor } from '../api/types'
import { INK_NAMES } from '../lib/schedule'
import { Sparkline } from './charts'
import { CheckIcon, EyeIcon, LockIcon } from './icons'

/**
 * Small primitives. Class names are the contract with styles.css; variants
 * are data-attributes so one class carries the shape and attributes carry the
 * tone, which keeps selector specificity flat.
 */

type ButtonProps = ComponentProps<'button'> & {
  variant?: 'primary' | 'soft' | 'ghost' | 'danger'
  pending?: boolean
}

export function Button({ variant = 'primary', pending = false, className, children, disabled, ...rest }: ButtonProps) {
  return (
    <button
      type="button"
      className={`button ${className ?? ''}`}
      data-variant={variant}
      disabled={disabled === true || pending}
      data-pending={pending ? 'true' : undefined}
      {...rest}
    >
      {pending ? <span className="spinner" aria-hidden="true" /> : null}
      {children}
    </button>
  )
}

type IconButtonProps = ComponentProps<'button'> & { label: string }

export function IconButton({ label, className, children, ...rest }: IconButtonProps) {
  return (
    <button
      type="button"
      className={`icon-button ${className ?? ''}`}
      aria-label={label}
      title={label}
      {...rest}
    >
      {children}
    </button>
  )
}

export function Field({
  label,
  htmlFor,
  error,
  hint,
  children,
}: {
  label: string
  htmlFor?: string
  error?: string
  hint?: string
  children: ReactNode
}) {
  return (
    <div className="field" data-invalid={error !== undefined && error !== '' ? 'true' : undefined}>
      <label className="field-label" htmlFor={htmlFor}>
        {label}
      </label>
      {children}
      {error !== undefined && error !== '' ? (
        <p className="field-error" role="alert">
          {error}
        </p>
      ) : hint !== undefined && hint !== '' ? (
        <p className="field-hint">{hint}</p>
      ) : null}
    </div>
  )
}

export function Input({ className, ...rest }: ComponentProps<'input'>) {
  return <input className={`input ${className ?? ''}`} {...rest} />
}

export function Textarea({ className, ...rest }: ComponentProps<'textarea'>) {
  return <textarea className={`input textarea ${className ?? ''}`} {...rest} />
}

export function Segmented<T extends string>({
  options,
  value,
  onChange,
  ariaLabel,
}: {
  options: { value: T; label: ReactNode }[]
  value: T
  onChange: (value: T) => void
  ariaLabel: string
}) {
  return (
    <div className="segmented" role="group" aria-label={ariaLabel}>
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          className="segmented-option"
          data-active={option.value === value ? 'true' : undefined}
          aria-pressed={option.value === value}
          onClick={() => onChange(option.value)}
        >
          {option.label}
        </button>
      ))}
    </div>
  )
}

export function Chip({
  active = false,
  onClick,
  children,
}: {
  active?: boolean
  onClick?: () => void
  children: ReactNode
}) {
  return (
    <button type="button" className="chip" data-active={active ? 'true' : undefined} onClick={onClick}>
      {children}
    </button>
  )
}

export function Switch({
  checked,
  onChange,
  label,
  hint,
}: {
  checked: boolean
  onChange: (checked: boolean) => void
  label: string
  hint?: string
}) {
  return (
    <label className="switch-row">
      <span className="switch-text">
        <span className="switch-label">{label}</span>
        {hint !== undefined ? <span className="switch-hint">{hint}</span> : null}
      </span>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={label}
        className="switch"
        data-on={checked ? 'true' : undefined}
        onClick={() => onChange(!checked)}
      >
        <span className="switch-thumb" />
      </button>
    </label>
  )
}

export function EmptyState({
  icon,
  title,
  message,
  action,
}: {
  icon: ReactNode
  title: string
  message: string
  action?: ReactNode
}) {
  return (
    <div className="empty">
      <span className="empty-icon" aria-hidden="true">
        {icon}
      </span>
      <h3 className="empty-title">{title}</h3>
      <p className="empty-message">{message}</p>
      {action}
    </div>
  )
}

export function StatTile({
  label,
  value,
  sub,
  icon,
  spark,
}: {
  label: string
  value: ReactNode
  sub?: string
  icon?: ReactNode
  /** Optional 14-day magnitude line, drawn in the brand hue. */
  spark?: number[]
}) {
  return (
    <div className="stat-tile">
      <span className="stat-label">
        {icon}
        {label}
      </span>
      <span className="stat-value">{value}</span>
      {sub !== undefined ? <span className="stat-sub">{sub}</span> : null}
      {spark !== undefined ? (
        <span className="tile-spark">
          <Sparkline values={spark} width={104} height={26} label={`${label}近 14 天`} />
        </span>
      ) : null}
    </div>
  )
}

/**
 * Explicit visibility badge: admins must be able to tell at a glance which
 * projects anonymous visitors can see. Icon + word, never color alone.
 */
export function VisibilityBadge({ isPrivate }: { isPrivate: boolean }) {
  return (
    <span className="vis-badge" data-private={isPrivate ? 'true' : undefined}>
      {isPrivate ? <LockIcon size={11} /> : <EyeIcon size={11} />}
      {isPrivate ? '私有' : '公开'}
    </span>
  )
}

/** The eight validated inks as pickable swatches. Names, not color alone. */
export function ColorPicker({ value, onChange }: { value: InkColor; onChange: (color: InkColor) => void }) {
  return (
    <div className="color-picker" role="group" aria-label="颜色">
      {INK_COLORS.map((color) => (
        <button
          key={color}
          type="button"
          className="color-swatch"
          data-ink={color}
          data-active={color === value ? 'true' : undefined}
          aria-label={INK_NAMES[color]}
          aria-pressed={color === value}
          title={INK_NAMES[color]}
          onClick={() => onChange(color)}
        >
          {color === value ? <CheckIcon size={14} strokeWidth={2.6} /> : null}
        </button>
      ))}
    </div>
  )
}

const EMOJI_CHOICES = [
  '💧', '🏃', '🧘', '🏋️', '🚶', '🚴', '🏊', '⚽',
  '📚', '🧠', '📝', '✍️', '🎧', '🎹', '🎨', '🗣️',
  '💻', '📖', '🔬', '🧮', '🌅', '🌙', '😴', '🛏️',
  '🥗', '🍎', '🥦', '🍵', '☕', '🚭', '💊', '🦷',
  '💰', '🧹', '🌱', '🐕', '📵', '🙏', '❤️', '🔥',
  '⭐', '🎯', '🏆', '💪', '🧴', '👟', '🎸', '📷',
]

/** Curated emoji grid — a project's face, kept small enough to scan. */
export function EmojiPicker({ value, onChange }: { value: string; onChange: (emoji: string) => void }) {
  return (
    <div className="emoji-picker" role="group" aria-label="图标">
      {EMOJI_CHOICES.map((emoji) => (
        <button
          key={emoji}
          type="button"
          className="emoji-option"
          data-active={emoji === value ? 'true' : undefined}
          aria-label={`图标 ${emoji}`}
          aria-pressed={emoji === value}
          onClick={() => onChange(emoji)}
        >
          {emoji}
        </button>
      ))}
    </div>
  )
}
