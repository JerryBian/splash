import { useEffect, useId, useRef, type ReactNode } from 'react'
import { useApp } from '../hooks/useApp'
import { AlertIcon, CheckIcon, InfoIcon, XIcon } from './icons'
import { Button, IconButton } from './ui'

/**
 * A bottom sheet: the app's one overlay shape. Slides up over a dimmed
 * backdrop, locks body scroll, closes on Escape and backdrop click. Motion
 * answers the open action; prefers-reduced-motion disables it in CSS.
 *
 * The backdrop is a real <button> sibling of the sheet (not a clickable div):
 * keyboard and screen-reader users get a genuine "close" control, and nesting
 * stays valid HTML even though the sheet is full of buttons.
 */
export function Sheet({
  open,
  onClose,
  title,
  children,
  footer,
}: {
  open: boolean
  onClose: () => void
  title: ReactNode
  children: ReactNode
  footer?: ReactNode
}) {
  const sheetRef = useRef<HTMLDivElement>(null)
  const titleId = useId()

  useEffect(() => {
    if (!open) return
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = previousOverflow
    }
  }, [open, onClose])

  // Focus enters the sheet on open and returns to the control that opened it
  // on close. Kept separate from the key listener so a re-rendered inline
  // onClose callback cannot bounce focus around while the sheet is open.
  useEffect(() => {
    if (!open) return
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
    sheetRef.current?.focus()
    return () => previous?.focus()
  }, [open])

  if (!open) return null

  return (
    <div className="sheet-layer">
      <button type="button" className="sheet-backdrop" aria-label="关闭弹层" onClick={onClose} />
      <div
        className="sheet"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        ref={sheetRef}
        tabIndex={-1}
      >
        <div className="sheet-handle" aria-hidden="true" />
        <header className="sheet-head">
          <h2 className="sheet-title" id={titleId}>
            {title}
          </h2>
          <IconButton label="关闭" onClick={onClose}>
            <XIcon />
          </IconButton>
        </header>
        <div className="sheet-body">{children}</div>
        {footer !== undefined ? <footer className="sheet-foot">{footer}</footer> : null}
      </div>
    </div>
  )
}

/** A small centered confirmation for destructive actions. */
export function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel = '确认',
  danger = false,
  pending = false,
  confirmDisabled = false,
  children,
  onConfirm,
  onClose,
}: {
  open: boolean
  title: string
  message: string
  confirmLabel?: string
  danger?: boolean
  pending?: boolean
  confirmDisabled?: boolean
  /** Extra controls between the message and the buttons, e.g. a password field. */
  children?: ReactNode
  onConfirm: () => void
  onClose: () => void
}) {
  useEffect(() => {
    if (!open) return
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null
  return (
    <div className="dialog-layer">
      <button type="button" className="dialog-backdrop" aria-label="关闭对话框" onClick={onClose} />
      <div className="dialog" role="alertdialog" aria-modal="true" aria-label={title}>
        <h2 className="dialog-title">{title}</h2>
        <p className="dialog-message">{message}</p>
        {children}
        <div className="dialog-actions">
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button
            variant={danger ? 'danger' : 'primary'}
            pending={pending}
            disabled={confirmDisabled}
            onClick={onConfirm}
          >
            {confirmLabel}
          </Button>
        </div>
      </div>
    </div>
  )
}

const TOAST_ICONS = {
  ok: <CheckIcon size={16} strokeWidth={2.4} />,
  error: <AlertIcon size={16} />,
  info: <InfoIcon size={16} />,
} as const

/** Renders the store's toast queue above everything except the splatter. */
export function ToastHost() {
  const { toasts, dismissToast } = useApp()
  if (toasts.length === 0) return null
  return (
    <div className="toast-host" role="status" aria-live="polite">
      {toasts.map((toast) => (
        <button key={toast.id} type="button" className="toast" data-tone={toast.tone} onClick={() => dismissToast(toast.id)}>
          <span className="toast-icon">{TOAST_ICONS[toast.tone]}</span>
          {toast.message}
        </button>
      ))}
    </div>
  )
}
