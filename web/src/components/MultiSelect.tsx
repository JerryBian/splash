import { useEffect, useId, useRef, useState } from 'react'
import { ChevronDownIcon } from './icons'

/** One selectable project in a link picker. */
export type MultiSelectOption = { id: string; icon: string; name: string }

/**
 * A compact checkbox multi-select for project relations.
 *
 * Project lists grow quickly, and a wall of chips stops being scannable. This
 * keeps the form one line: the trigger reports the count, a short summary names
 * at most two selections, and the popover adds search plus real checkboxes for
 * the full list. It closes on outside click and Escape.
 */
export function MultiSelect({
  placeholder,
  searchPlaceholder = '搜索项目…',
  options,
  selected,
  onToggle,
  emptyLabel = '没有可选项',
}: {
  placeholder: string
  searchPlaceholder?: string
  options: MultiSelectOption[]
  selected: string[]
  onToggle: (id: string) => void
  emptyLabel?: string
}) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const root = useRef<HTMLDivElement>(null)
  const search = useRef<HTMLInputElement>(null)
  const panelID = useId()

  useEffect(() => {
    if (!open) return
    search.current?.focus()
    const onPointer = (event: PointerEvent) => {
      if (root.current !== null && !root.current.contains(event.target as Node)) {
        setOpen(false)
      }
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }
    document.addEventListener('pointerdown', onPointer)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('pointerdown', onPointer)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const selectedOptions = options.filter((option) => selected.includes(option.id))
  const needle = query.trim().toLowerCase()
  const filtered = needle === '' ? options : options.filter((option) => option.name.toLowerCase().includes(needle))

  return (
    <div className="multi-select" ref={root}>
      <button
        type="button"
        className="multi-select-trigger"
        aria-label={placeholder}
        aria-expanded={open}
        aria-controls={panelID}
        onClick={() => {
          setOpen((current) => !current)
          setQuery('')
        }}
      >
        <span>{selected.length > 0 ? `已选 ${selected.length} 项` : placeholder}</span>
        <ChevronDownIcon size={15} />
      </button>
      {selectedOptions.length > 0 ? (
        <span className="multi-select-summary">
          {selectedOptions.slice(0, 2).map((option) => option.name).join('、')}
          {selectedOptions.length > 2 ? ` 等 ${selectedOptions.length} 项` : ''}
        </span>
      ) : null}

      {open ? (
        <div id={panelID} className="multi-select-pop" role="group" aria-label={placeholder}>
          <input
            ref={search}
            className="input multi-select-search"
            type="search"
            value={query}
            placeholder={searchPlaceholder}
            aria-label={searchPlaceholder}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={(event) => {
              // The picker sits inside the project form; Enter while searching
              // must not submit the project.
              if (event.key === 'Enter') event.preventDefault()
            }}
          />
          <div className="multi-select-list">
            {filtered.length === 0 ? (
              <p className="sheet-note">{emptyLabel}</p>
            ) : (
              filtered.map((option) => {
                const checked = selected.includes(option.id)
                return (
                  <label key={option.id} className="multi-select-option" data-checked={checked ? 'true' : undefined}>
                    <input type="checkbox" checked={checked} onChange={() => onToggle(option.id)} />
                    <span className="multi-select-name">
                      <span aria-hidden="true">{option.icon}</span> {option.name}
                    </span>
                  </label>
                )
              })
            )}
          </div>
          <div className="multi-select-foot">
            <span>已选 {selected.length} 项</span>
            {selected.length > 0 ? (
              <button
                type="button"
                className="text-toggle"
                onClick={() => {
                  for (const id of selected) onToggle(id)
                }}
              >
                清空
              </button>
            ) : null}
          </div>
        </div>
      ) : null}
    </div>
  )
}
