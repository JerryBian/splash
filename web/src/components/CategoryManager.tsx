import { useState } from 'react'
import { api } from '../api/client'
import type { Category, InkColor } from '../api/types'
import { useApp } from '../hooks/useApp'
import { describeError, fieldErrorsFrom } from '../lib/errors'
import { PencilIcon, PlusIcon, TrashIcon } from './icons'
import { ConfirmDialog } from './overlays'
import { Button, ColorPicker, Field, IconButton, Input } from './ui'

/**
 * Categories: create, rename, recolor, delete. Deleting a category never
 * deletes its projects — they become uncategorized, which the server
 * enforces with ON DELETE SET NULL.
 */
export function CategoryManager() {
  const { categories, refresh, toast } = useApp()
  const [editing, setEditing] = useState<Category | 'new' | null>(null)
  const [doomed, setDoomed] = useState<Category | null>(null)
  const [busy, setBusy] = useState(false)

  const remove = async () => {
    if (doomed === null) return
    setBusy(true)
    try {
      await api.deleteCategory(doomed.id)
      toast(`已删除分类「${doomed.name}」，其项目变为未分类`, 'ok')
      setDoomed(null)
      await refresh()
    } catch (error) {
      toast(describeError(error), 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="cat-manager">
      {categories.length === 0 ? (
        <p className="sheet-note">还没有分类。分类让今日页按组展示项目。</p>
      ) : (
        <ul className="cat-list">
          {categories.map((category) => (
            <li key={category.id} className="cat-row" data-ink={category.color}>
              <span className="cat-icon" aria-hidden="true">
                {category.icon}
              </span>
              <span className="cat-name">{category.name}</span>
              <span className="cat-dot" aria-hidden="true" />
              <IconButton label={`编辑分类 ${category.name}`} onClick={() => setEditing(category)}>
                <PencilIcon size={15} />
              </IconButton>
              <IconButton label={`删除分类 ${category.name}`} onClick={() => setDoomed(category)}>
                <TrashIcon size={15} />
              </IconButton>
            </li>
          ))}
        </ul>
      )}

      {editing === null ? (
        <Button variant="soft" onClick={() => setEditing('new')}>
          <PlusIcon size={15} /> 新建分类
        </Button>
      ) : (
        <CategoryForm
          key={editing === 'new' ? 'new' : editing.id}
          category={editing === 'new' ? null : editing}
          onDone={() => setEditing(null)}
        />
      )}

      <ConfirmDialog
        open={doomed !== null}
        title={doomed === null ? '' : `删除分类「${doomed.name}」？`}
        message="分类下的项目不会被删除，它们会变为未分类。"
        confirmLabel="删除"
        danger
        pending={busy}
        onConfirm={() => void remove()}
        onClose={() => setDoomed(null)}
      />
    </div>
  )
}

function CategoryForm({ category, onDone }: { category: Category | null; onDone: () => void }) {
  const { refresh, toast } = useApp()
  const [name, setName] = useState(category?.name ?? '')
  const [icon, setIcon] = useState(category?.icon ?? '🗂️')
  const [color, setColor] = useState<InkColor>(category?.color ?? 'blue')
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    if (name.trim() === '') {
      setErrors({ name: '请填写分类名称' })
      return
    }
    setBusy(true)
    setErrors({})
    try {
      if (category === null) {
        await api.createCategory({ name: name.trim(), icon, color })
        toast('分类已创建', 'ok')
      } else {
        await api.updateCategory(category.id, { name: name.trim(), icon, color })
        toast('分类已更新', 'ok')
      }
      await refresh()
      onDone()
    } catch (error) {
      const fields = fieldErrorsFrom(error)
      setErrors(Object.keys(fields).length > 0 ? fields : { name: describeError(error) })
    } finally {
      setBusy(false)
    }
  }

  return (
    <form
      className="cat-form"
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
    >
      <Field label="名称" htmlFor="cat-name" error={errors['name']}>
        <Input id="cat-name" value={name} maxLength={40} onChange={(event) => setName(event.target.value)} />
      </Field>
      <Field label="图标">
        <div className="emoji-picker compact" role="group" aria-label="分类图标">
          {['🗂️', '💪', '📖', '🏡', '💼', '🎨', '🌱', '❤️', '🧠', '⚡', '🍀', '🎯'].map((emoji) => (
            <button
              key={emoji}
              type="button"
              className="emoji-option"
              data-active={emoji === icon ? 'true' : undefined}
              aria-pressed={emoji === icon}
              aria-label={`图标 ${emoji}`}
              onClick={() => setIcon(emoji)}
            >
              {emoji}
            </button>
          ))}
        </div>
      </Field>
      <Field label="颜色">
        <ColorPicker value={color} onChange={setColor} />
      </Field>
      <div className="form-actions">
        <Button variant="ghost" onClick={onDone}>
          取消
        </Button>
        <Button type="submit" pending={busy}>
          {category === null ? '创建' : '保存'}
        </Button>
      </div>
    </form>
  )
}
