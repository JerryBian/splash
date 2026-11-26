import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { MultiSelect } from './MultiSelect'

const options = [
  { id: 'read', icon: '📖', name: '阅读' },
  { id: 'sleep', icon: '😴', name: '准时睡觉' },
  { id: 'run', icon: '🏃', name: '晨跑' },
]
const selectedIds = ['read']

describe('MultiSelect', () => {
  it('reports the count, searches, checks a box and closes on Escape', async () => {
    const onToggle = vi.fn()
    render(<MultiSelect placeholder="选择项目" options={options} selected={selectedIds} onToggle={onToggle} />)

    expect(screen.getByRole('button', { name: '选择项目' })).toHaveTextContent('已选 1 项')
    expect(screen.getByText('阅读')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: '选择项目' }))
    const panel = screen.getByRole('group', { name: '选择项目' })
    expect(within(panel).getByRole('checkbox', { name: /阅读/ })).toBeChecked()

    await userEvent.type(screen.getByRole('searchbox'), '睡')
    expect(within(panel).queryByText('阅读')).toBeNull()
    await userEvent.click(within(panel).getByRole('checkbox', { name: /准时睡觉/ }))
    expect(onToggle).toHaveBeenCalledWith('sleep')

    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('group', { name: '选择项目' })).toBeNull()
  })
})
