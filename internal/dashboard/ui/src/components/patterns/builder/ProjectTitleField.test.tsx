import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'
import { ProjectTitleField } from './ProjectTitleField'

function setup(value = 'Agenda da Clínica') {
  const onSave = vi.fn()
  render(<ProjectTitleField value={value} onSave={onSave} />)
  return onSave
}

const editButton = () => screen.getByRole('button', { name: /rename project|renomear projeto/i })
const input = () => screen.getByRole('textbox')

describe('ProjectTitleField', () => {
  test('shows the title and edits it with Enter', () => {
    const onSave = setup()
    expect(screen.getByText('Agenda da Clínica')).toBeInTheDocument()
    fireEvent.click(editButton())
    fireEvent.change(input(), { target: { value: '  Clínica do Zé  ' } })
    fireEvent.keyDown(input(), { key: 'Enter' })
    expect(onSave).toHaveBeenCalledTimes(1)
    expect(onSave).toHaveBeenCalledWith('Clínica do Zé')
  })

  test('Escape cancels without saving, even when the field then loses focus', () => {
    const onSave = setup()
    fireEvent.click(editButton())
    fireEvent.change(input(), { target: { value: 'Outro nome' } })
    fireEvent.keyDown(input(), { key: 'Escape' })
    expect(onSave).not.toHaveBeenCalled()
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
  })

  test('leaving the field saves, an unchanged or empty value does not', () => {
    const onSave = setup()
    fireEvent.click(editButton())
    fireEvent.change(input(), { target: { value: 'Novo nome' } })
    fireEvent.blur(input())
    expect(onSave).toHaveBeenCalledWith('Novo nome')

    fireEvent.click(editButton())
    fireEvent.blur(input())
    expect(onSave).toHaveBeenCalledTimes(1)

    fireEvent.click(editButton())
    fireEvent.change(input(), { target: { value: '   ' } })
    fireEvent.keyDown(input(), { key: 'Enter' })
    expect(onSave).toHaveBeenCalledTimes(1)
  })
})
