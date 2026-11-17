import { describe, test, expect, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom'
import { ModeSwitch } from './ModeSwitch'
import { MODE_STORAGE_KEY, resetModeMemory } from '@/lib/mode'

function Where() {
  return <output data-testid="where">{useLocation().pathname}</output>
}

function setup(advanced: boolean, start = '/apps') {
  return render(
    <MemoryRouter initialEntries={[start]}>
      <ModeSwitch advanced={advanced} />
      <Routes>
        <Route path="*" element={<Where />} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => resetModeMemory())

describe('ModeSwitch', () => {
  test('is a labelled switch whose state follows the shell', () => {
    setup(true)
    const sw = screen.getByRole('switch', { name: 'Advanced mode' })
    expect(sw).toHaveAttribute('aria-checked', 'true')
  })

  test('clicking the visible label toggles the switch too', async () => {
    setup(false, '/')
    await userEvent.click(screen.getByText('Advanced mode'))
    expect(screen.getByTestId('where').textContent).toBe('/apps')
  })

  test('turning it on saves advanced and goes to /apps', async () => {
    setup(false, '/')
    await userEvent.click(screen.getByRole('switch', { name: 'Advanced mode' }))
    expect(localStorage.getItem(MODE_STORAGE_KEY)).toBe('advanced')
    expect(screen.getByTestId('where').textContent).toBe('/apps')
  })

  test('turning it off saves simple and goes to /', async () => {
    setup(true, '/apps')
    await userEvent.click(screen.getByRole('switch', { name: 'Advanced mode' }))
    expect(localStorage.getItem(MODE_STORAGE_KEY)).toBe('simple')
    expect(screen.getByTestId('where').textContent).toBe('/')
  })

  test('works from the keyboard (Tab, then Space)', async () => {
    setup(true, '/apps')
    await userEvent.tab()
    expect(screen.getByRole('switch')).toHaveFocus()
    await userEvent.keyboard(' ')
    expect(localStorage.getItem(MODE_STORAGE_KEY)).toBe('simple')
  })

  test('does not throw when localStorage is blocked', async () => {
    const original = Storage.prototype.setItem
    Storage.prototype.setItem = () => {
      throw new Error('blocked')
    }
    try {
      setup(false, '/')
      await userEvent.click(screen.getByRole('switch'))
      expect(screen.getByTestId('where').textContent).toBe('/apps')
    } finally {
      Storage.prototype.setItem = original
    }
  })
})
