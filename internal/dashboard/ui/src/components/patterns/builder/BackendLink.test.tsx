import { describe, test, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { BackendLink } from './BackendLink'

const wrap = (id: string | null) =>
  render(
    <MemoryRouter>
      <BackendLink backendAppId={id} />
    </MemoryRouter>,
  )

describe('BackendLink', () => {
  test('a project with a backend links to the app, with the id encoded', () => {
    wrap('a/b c')
    expect(screen.getByRole('link', { name: 'Backend app' })).toHaveAttribute('href', '/apps/a%2Fb%20c')
    expect(screen.queryByText('No backend')).not.toBeInTheDocument()
  })

  test('a project with no backend says so and offers a project with one', () => {
    wrap(null)
    expect(screen.getByText('No backend')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Backend app' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Start a project with a backend' })).toHaveAttribute('href', '/?backend=new')
  })
})
