import { describe, test, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { BuilderSettings } from '@/lib/api'
import { hasUnsavedChanges } from '@/lib/useUnsavedChangesGuard'

// Only the unsaved-changes wiring is under test: every server hook answers a fixed value.
const settings: BuilderSettings = {
  enabled: true,
  provider: 'openai',
  context_window_tokens: 128000,
  session_token_cap: 100000,
  user_monthly_token_cap: 1000000,
  max_steps_per_turn: 40,
  max_fix_iterations: 3,
  max_turn_seconds: 600,
  cost_currency: 'USD',
  cost_currency_changed_at: null,
  cost_caps_review_pending: false,
  session_cost_cap_micros: 0,
  user_monthly_cost_cap_micros: 0,
  planning_mode: 'off',
  planning_model: '',
  updated_by: null,
  updated_at: null,
  configured: true,
}

const idle = { mutate: vi.fn(), isPending: false }

vi.mock('@/lib/useCurrentRole', () => ({ useCurrentRole: () => 'superadmin' }))
vi.mock('./BuilderSkillsSection', () => ({ BuilderSkillsSection: () => null }))
vi.mock('@/lib/api', async (orig) => ({
  ...(await orig<typeof import('@/lib/api')>()),
  authFetch: () => new Promise(() => {}),
  useBuilderSettings: () => ({ data: settings, isLoading: false, isError: false }),
  useUpdateBuilderSettings: () => idle,
  useBuilderCatalog: () => ({ data: undefined }),
  useAIProviderStatus: () => ({ data: undefined }),
  useBuilderPrompt: () => ({
    data: { persona: 'Be kind.', rules: 'No jargon.', max_chars: 8000 },
    isLoading: false,
    isError: false,
  }),
  useUpdateBuilderPrompt: () => idle,
  useBuilderPromptVersions: () => ({ data: undefined, isLoading: true }),
  useSetBuilderTemplateEnabled: () => idle,
  useBuilderPrices: () => ({
    data: { currency: 'USD', prices: [], unpriced_models: [] },
    isLoading: false,
    isError: false,
  }),
  useUpsertBuilderPrice: () => idle,
  useDeleteBuilderPrice: () => idle,
}))

import AIBuilderSettings from './AIBuilderSettings'

function setup() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const view = render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AIBuilderSettings />
      </MemoryRouter>
    </QueryClientProvider>,
  )
  const field = (id: string) => {
    const el = view.container.querySelector<HTMLInputElement | HTMLTextAreaElement>(`#${id}`)
    if (!el) throw new Error(`no #${id}`)
    return el
  }
  return { field, user: userEvent.setup() }
}

describe('AI Builder settings: unsaved changes guard', () => {
  test('a pristine page guards nothing', () => {
    setup()
    expect(hasUnsavedChanges()).toBe(false)
  })

  test('general limits: editing guards, putting the value back releases', async () => {
    const { field, user } = setup()
    const input = field('builder-limit-max_steps_per_turn')
    await user.clear(input)
    await user.type(input, '41')
    expect(hasUnsavedChanges()).toBe(true)
    await user.clear(input)
    await user.type(input, '40')
    expect(hasUnsavedChanges()).toBe(false)
  })

  test('persona and rules: editing guards, discarding releases', async () => {
    const { field, user } = setup()
    await user.type(field('builder-persona'), ' Always.')
    expect(hasUnsavedChanges()).toBe(true)
    await user.clear(field('builder-persona'))
    await user.type(field('builder-persona'), 'Be kind.')
    expect(hasUnsavedChanges()).toBe(false)
  })

  test('money caps: editing guards', async () => {
    const { field, user } = setup()
    await user.type(field('builder-cost-session_cost_cap_micros'), '5')
    expect(hasUnsavedChanges()).toBe(true)
    await user.clear(field('builder-cost-session_cost_cap_micros'))
    expect(hasUnsavedChanges()).toBe(false)
  })

  test('price form: opening it guards nothing, typing guards, cancelling releases', async () => {
    const { field, user } = setup()
    await user.click(screen.getByRole('button', { name: 'Add price' }))
    expect(hasUnsavedChanges()).toBe(false)
    await user.type(field('builder-price-model'), 'my-model')
    expect(hasUnsavedChanges()).toBe(true)
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(hasUnsavedChanges()).toBe(false)
  })
})
