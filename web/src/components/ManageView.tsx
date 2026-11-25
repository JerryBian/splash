import { useDeferredValue, useMemo, useState } from 'react'
import { api } from '../api/client'
import type { Project } from '../api/types'
import { useApp } from '../hooks/useApp'
import { navigate } from '../hooks/useNow'
import { describeError } from '../lib/errors'
import { scheduleLabel } from '../lib/format'
import {
  ArchiveIcon,
  LayersIcon,
  PencilIcon,
  PlusIcon,
  SearchIcon,
  SlidersIcon,
  TrashIcon,
  XIcon,
} from './icons'
import { CategoryManager } from './CategoryManager'
import { ConfirmDialog } from './overlays'
import { Button, Chip, EmptyState, IconButton, Input, NatureBadge, Switch } from './ui'

/**
 * The admin workshop: every project including archived ones, searchable and
 * filterable, with edit / archive / delete at hand, and the category manager
 * below. Search uses useDeferredValue so typing stays responsive even with a
 * long list.
 */
export function ManageView() {
  const { projects, categories, refresh, toast } = useApp()
  const [search, setSearch] = useState('')
  const [categoryFilter, setCategoryFilter] = useState('all')
  const [showArchived, setShowArchived] = useState(true)
  const [doomed, setDoomed] = useState<Project | null>(null)
  const [busy, setBusy] = useState(false)
  const deferredSearch = useDeferredValue(search)

  // Keep the list meaningful if the selected category disappears.
  const activeCategory =
    categoryFilter === 'all' || categories.some((category) => category.id === categoryFilter)
      ? categoryFilter
      : 'all'

  const visible = useMemo(() => {
    const needle = deferredSearch.trim().toLowerCase()
    return projects.filter((p) => {
      if (!showArchived && p.archived) return false
      if (activeCategory !== 'all' && p.categoryId !== activeCategory) return false
      if (needle === '') return true
      return (
        p.name.toLowerCase().includes(needle) ||
        (p.description ?? '').toLowerCase().includes(needle)
      )
    })
  }, [projects, deferredSearch, activeCategory, showArchived])

  const toggleArchive = async (project: Project) => {
    try {
      await api.updateProject(project.id, { archived: !project.archived }, `"v${project.version}"`)
      toast(project.archived ? '已恢复项目' : '已归档项目', 'ok')
      await refresh()
    } catch (error) {
      toast(describeError(error), 'error')
    }
  }

  const doDelete = async () => {
    if (doomed === null) return
    setBusy(true)
    try {
      await api.deleteProject(doomed.id)
      toast(`已删除「${doomed.name}」`, 'ok')
      setDoomed(null)
      await refresh()
    } catch (error) {
      toast(describeError(error), 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="view manage">
      <header className="manage-head">
        <h1 className="view-title">管理</h1>
        <Button onClick={() => navigate('#edit/new')}>
          <PlusIcon size={16} /> 新建项目
        </Button>
      </header>

      <div className="search-row">
        <SearchIcon size={16} className="search-icon" />
        <Input
          type="search"
          value={search}
          placeholder="搜索项目名称或说明…"
          aria-label="搜索项目"
          onChange={(event) => setSearch(event.target.value)}
        />
        {search !== '' ? (
          <IconButton label="清除搜索" className="search-clear" onClick={() => setSearch('')}>
            <XIcon size={15} />
          </IconButton>
        ) : null}
      </div>

      <div className="chip-row" role="group" aria-label="按分类筛选">
        <Chip active={activeCategory === 'all'} onClick={() => setCategoryFilter('all')}>
          全部
        </Chip>
        {categories.map((c) => (
          <Chip key={c.id} active={activeCategory === c.id} onClick={() => setCategoryFilter(c.id)}>
            <span aria-hidden="true">{c.icon}</span> {c.name}
          </Chip>
        ))}
      </div>

      <div className="manage-toolbar">
        <Switch checked={showArchived} onChange={setShowArchived} label="显示已归档" />
        <span className="manage-count" aria-live="polite">
          {visible.length} 个
        </span>
      </div>

      {visible.length === 0 ? (
        <EmptyState
          icon={<SlidersIcon size={30} />}
          title="没有匹配的项目"
          message="换个关键词，或创建一个新的打卡项目。"
          action={
            <Button variant="soft" onClick={() => navigate('#edit/new')}>
              <PlusIcon size={16} /> 新建项目
            </Button>
          }
        />
      ) : (
        <ul className="manage-list">
          {visible.map((project) => {
            const category = categories.find((c) => c.id === project.categoryId)
            return (
              <li key={project.id} className="manage-row" data-ink={project.color} data-archived={project.archived ? 'true' : undefined}>
                <button type="button" className="manage-open" onClick={() => navigate(`#project/${project.id}`)}>
                  <span className="manage-icon" aria-hidden="true">
                    {project.icon}
                  </span>
                  <span className="manage-main">
                    <span className="manage-name">
                      {/* The name ellipsizes; badges keep their full shape so
                          the row stays one line even at 320px. */}
                      <span className="manage-project-name">{project.name}</span>
                      <NatureBadge nature={project.nature} />
                      {project.archived ? <span className="mini-chip">已归档</span> : null}
                    </span>
                    <span className="manage-sub">
                      {scheduleLabel(project)}
                      {category !== undefined ? ` · ${category.name}` : ''}
                    </span>
                  </span>
                </button>
                <span className="manage-actions">
                  <IconButton label={`编辑 ${project.name}`} onClick={() => navigate(`#edit/${project.id}`)}>
                    <PencilIcon size={16} />
                  </IconButton>
                  <IconButton
                    label={project.archived ? `恢复 ${project.name}` : `归档 ${project.name}`}
                    onClick={() => void toggleArchive(project)}
                  >
                    <ArchiveIcon size={16} />
                  </IconButton>
                  <IconButton label={`删除 ${project.name}`} onClick={() => setDoomed(project)}>
                    <TrashIcon size={16} />
                  </IconButton>
                </span>
              </li>
            )
          })}
        </ul>
      )}

      <section className="detail-section panel chart-panel category-panel" aria-label="分类管理">
        <h2 className="section-head">
          <LayersIcon size={15} /> 分类管理
        </h2>
        <CategoryManager />
      </section>

      <ConfirmDialog
        open={doomed !== null}
        title={doomed === null ? '' : `删除「${doomed.name}」？`}
        message="所有打卡记录和跳过标记都会一并删除，无法恢复。"
        confirmLabel="删除"
        danger
        pending={busy}
        onConfirm={() => void doDelete()}
        onClose={() => setDoomed(null)}
      />
    </div>
  )
}
