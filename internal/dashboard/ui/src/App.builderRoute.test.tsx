import { describe, test, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { MemoryRouter, Routes, Route, Link, useParams } from 'react-router-dom'

// BuilderPage and everything under it (ChatPanel draft, PreviewPanel token and iframe,
// QuestionCard answers, SessionActions publish result, restoreSeq) keep local state. The
// stub stands in for all of it: one piece of local state plus the session it was created for.
const mounts = vi.hoisted(() => ({ count: 0 }))
vi.mock('@/pages/BuilderPage', () => ({
  default: function StubBuilderPage() {
    const { sessionId } = useParams()
    const [createdFor] = useState(() => {
      mounts.count++
      return sessionId
    })
    const [draft, setDraft] = useState('')
    return (
      <div>
        <p data-testid="created-for">{createdFor}</p>
        <label>
          draft
          <input value={draft} onChange={(e) => setDraft(e.target.value)} />
        </label>
      </div>
    )
  },
}))

import { BuilderRoute } from './App'

describe('/builder/:sessionId', () => {
  test('switching from session A to B starts B from a clean page', async () => {
    const user = userEvent.setup()
    render(
      <MemoryRouter initialEntries={['/builder/A']}>
        <Link to="/builder/B">open B</Link>
        <Routes>
          <Route path="/builder/:sessionId" element={<BuilderRoute />} />
        </Routes>
      </MemoryRouter>,
    )

    expect(screen.getByTestId('created-for')).toHaveTextContent('A')
    await user.type(screen.getByLabelText('draft'), 'half-written message for A')

    await user.click(screen.getByRole('link', { name: 'open B' }))

    expect(screen.getByTestId('created-for')).toHaveTextContent('B')
    expect(screen.getByLabelText('draft')).toHaveValue('')
    expect(mounts.count).toBe(2)
  })
})
