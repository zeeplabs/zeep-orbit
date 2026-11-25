import { describe, test, expect, vi, beforeEach } from 'vitest'
import { render, screen, within, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom'

const h = vi.hoisted(() => ({
  mutate: vi.fn(),
  isPending: false,
  apps: [] as { id: string; name: string }[],
  sessions: [] as unknown[],
}))

vi.mock('@/lib/api', async (orig) => ({
  ...(await orig<typeof import('@/lib/api')>()),
  useApps: () => ({ data: h.apps }),
  useBuilderSessions: () => ({ data: h.sessions }),
  useStartBuilderProject: () => ({ mutate: h.mutate, isPending: h.isPending }),
}))

import HomeChatPage from './HomeChatPage'

function Where() {
  return <output data-testid="where">{useLocation().pathname}</output>
}

function setup(entry = '/') {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <Routes>
        <Route path="/" element={<HomeChatPage />} />
        <Route path="*" element={<Where />} />
      </Routes>
    </MemoryRouter>,
  )
}

const box = () => screen.getByRole('textbox', { name: 'Describe your app' })
const send = () => screen.getByRole('button', { name: 'Start project' })

beforeEach(() => {
  h.mutate.mockReset()
  h.isPending = false
  h.apps = []
  h.sessions = []
})

describe('HomeChatPage composer', () => {
  test('shows the heading and a labelled textarea', () => {
    setup()
    expect(screen.getByRole('heading', { level: 1, name: 'What are we building today?' })).toBeInTheDocument()
    expect(box().tagName).toBe('TEXTAREA')
  })

  test('the send button is disabled for an empty or whitespace prompt', async () => {
    setup()
    expect(send()).toBeDisabled()
    await userEvent.type(box(), '   ')
    expect(send()).toBeDisabled()
    await userEvent.type(box(), 'a')
    expect(send()).toBeEnabled()
  })

  test('Enter sends the trimmed prompt with no backend by default and the UI language', async () => {
    setup()
    await userEvent.type(box(), '  an app for my clinic  {Enter}')
    expect(h.mutate).toHaveBeenCalledTimes(1)
    expect(h.mutate.mock.calls[0][0]).toEqual({
      prompt: '  an app for my clinic  ',
      backend: { mode: 'none' },
      locale: 'en',
    })
  })

  test('Shift+Enter inserts a newline and does not send', async () => {
    setup()
    await userEvent.type(box(), 'line one{Shift>}{Enter}{/Shift}line two')
    expect(h.mutate).not.toHaveBeenCalled()
    expect(box()).toHaveValue('line one\nline two')
  })

  test('Enter while an IME is composing does not send', async () => {
    setup()
    await userEvent.type(box(), 'x')
    fireEvent.keyDown(box(), { key: 'Enter', isComposing: true })
    expect(h.mutate).not.toHaveBeenCalled()
  })

  test('clicking the send button sends', async () => {
    setup()
    await userEvent.type(box(), 'hello')
    await userEvent.click(send())
    expect(h.mutate).toHaveBeenCalledTimes(1)
  })

  test('an empty prompt never sends, not even with Enter', async () => {
    setup()
    await userEvent.type(box(), '{Enter}')
    expect(h.mutate).not.toHaveBeenCalled()
  })

  test('navigates to the builder when the project is created', async () => {
    h.mutate.mockImplementation((_input, opts) => opts.onSuccess({ id: 's42' }))
    setup()
    await userEvent.type(box(), 'hello{Enter}')
    expect(screen.getByTestId('where').textContent).toBe('/builder/s42')
  })

  test('on a failure the text stays and the user stays on the home', async () => {
    h.mutate.mockImplementation(() => undefined) // the hook toasts; onSuccess is never called
    setup()
    await userEvent.type(box(), 'a long prompt I do not want to lose{Enter}')
    expect(box()).toHaveValue('a long prompt I do not want to lose')
    expect(screen.queryByTestId('where')).not.toBeInTheDocument()
  })

  test('while a request is in flight it cannot be sent again', async () => {
    h.isPending = true
    setup()
    expect(screen.getByRole('button', { name: 'Creating your project…' })).toBeDisabled()
    await userEvent.type(box(), 'x{Enter}') // read-only while pending
    expect(h.mutate).not.toHaveBeenCalled()
  })
})

describe('HomeChatPage suggestions', () => {
  test('a chip fills the composer with a full example prompt and does not send', async () => {
    setup()
    const list = screen.getByRole('list', { name: 'Suggestions' })
    expect(within(list).getAllByRole('button')).toHaveLength(4)
    await userEvent.click(within(list).getByRole('button', { name: 'A task board for a team' }))
    const filled = (box() as HTMLTextAreaElement).value
    expect(filled).toContain('A task board for my team of six people')
    // A real brief, not the chip title: it names the data, who signs in and the look.
    expect(filled.length).toBeGreaterThan(200)
    expect(filled).toMatch(/signs in/)
    expect(box()).toHaveFocus()
    expect(h.mutate).not.toHaveBeenCalled()
  })
})

describe('HomeChatPage backend choice', () => {
  test('lists the existing backends under their own group and sends the chosen one', async () => {
    h.apps = [
      { id: 'a1', name: 'clinic' },
      { id: 'a2', name: 'shop' },
    ]
    setup()
    const select = screen.getByRole('combobox', { name: 'Backend' })
    expect(select).toHaveValue('__none__')
    expect(within(select).getByRole('group', { name: 'Use an existing backend' })).toBeInTheDocument()
    await userEvent.selectOptions(select, 'a2')
    await userEvent.type(box(), 'add a report{Enter}')
    expect(h.mutate.mock.calls[0][0].backend).toEqual({ mode: 'existing', app_id: 'a2' })
  })

  test('without apps there are only the no-backend and new-backend options, no-backend first', () => {
    setup()
    const select = screen.getByRole('combobox', { name: 'Backend' })
    const options = within(select).getAllByRole('option')
    expect(options.map((o) => o.textContent)).toEqual(['No backend (a site)', 'New backend'])
    expect(within(select).queryByRole('group')).not.toBeInTheDocument()
  })

  test('choosing a new backend sends mode new', async () => {
    setup()
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Backend' }), '__new__')
    await userEvent.type(box(), 'an app for my clinic{Enter}')
    expect(h.mutate.mock.calls[0][0].backend).toEqual({ mode: 'new' })
  })

  test('?backend=new (the link in a project with no backend) preselects a new backend', async () => {
    setup('/?backend=new')
    const select = screen.getByRole('combobox', { name: 'Backend' })
    expect(select).toHaveValue('__new__')
    await userEvent.type(box(), 'an app for my clinic{Enter}')
    expect(h.mutate.mock.calls[0][0].backend).toEqual({ mode: 'new' })
  })

  test('any other ?backend value keeps the default', () => {
    setup('/?backend=whatever')
    expect(screen.getByRole('combobox', { name: 'Backend' })).toHaveValue('__none__')
  })
})

describe('HomeChatPage continue section', () => {
  test('is hidden when there are no sessions', () => {
    setup()
    expect(screen.queryByRole('heading', { name: 'Pick up where you left off' })).not.toBeInTheDocument()
  })

  test('shows the three latest sessions and a link to all projects', () => {
    h.sessions = [
      { id: 'a', title: 'Oldest', updated_at: '2026-01-01T00:00:00Z' },
      { id: 'b', title: 'Newest', updated_at: '2026-04-01T00:00:00Z' },
      { id: 'c', title: 'Middle', updated_at: '2026-03-01T00:00:00Z' },
      { id: 'd', title: '', updated_at: '2026-02-01T00:00:00Z' },
    ]
    setup()
    const section = screen.getByRole('region', { name: 'Pick up where you left off' })
    const links = within(section).getAllByRole('link')
    expect(links.map((l) => l.getAttribute('href'))).toEqual(['/projetos', '/builder/b', '/builder/c', '/builder/d'])
    expect(within(section).getByRole('link', { name: /Untitled project/ })).toBeInTheDocument()
    expect(within(section).queryByText('Oldest')).not.toBeInTheDocument()
  })
})
