import { describe, test, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'

const toastError = vi.hoisted(() => vi.fn())
vi.mock('sonner', () => ({ toast: { error: toastError, success: vi.fn() } }))

import { useStartBuilderProject } from './api'

type Call = { method: string; url: string; body: Record<string, unknown> | null }

const json = (status: number, body: unknown) =>
  new Response(status === 204 ? null : JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

/** Routes the fetch stub by "METHOD path-suffix"; every call is recorded. */
function stubFetch(routes: Record<string, () => Response>) {
  const calls: Call[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      calls.push({ method, url, body: init?.body ? JSON.parse(init.body as string) : null })
      const key = Object.keys(routes).find((k) => {
        const [m, suffix] = k.split(' ')
        return m === method && url.endsWith(suffix)
      })
      if (!key) throw new Error(`unexpected ${method} ${url}`)
      return routes[key]()
    }),
  )
  return calls
}

function setup() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  )
  return { qc, ...renderHook(() => useStartBuilderProject(), { wrapper }) }
}

const session = { id: 's1' }
const prompt = '  Quero um app para a minha clínica  '

beforeEach(() => toastError.mockReset())
afterEach(() => vi.unstubAllGlobals())

describe('useStartBuilderProject', () => {
  test('creates the session with the prompt and locale, then posts the prompt', async () => {
    const calls = stubFetch({
      'POST /builder/sessions': () => json(201, session),
      'POST /builder/sessions/s1/messages': () => json(202, { id: 't1' }),
    })
    const { result } = setup()
    await act(async () => {
      await result.current.mutateAsync({ prompt, backend: { mode: 'new' }, locale: 'pt-BR' })
    })
    expect(calls.map((c) => `${c.method} ${c.url.replace('/dashboard/api', '')}`)).toEqual([
      'POST /builder/sessions',
      'POST /builder/sessions/s1/messages',
    ])
    expect(calls[0].body).toEqual({
      backend: { mode: 'new' },
      locale: 'pt-BR',
      prompt: 'Quero um app para a minha clínica',
    })
    expect(calls[1].body).toEqual({ content: 'Quero um app para a minha clínica' })
    await waitFor(() => expect(result.current.data).toEqual(session))
    expect(toastError).not.toHaveBeenCalled()
  })

  test('an existing backend is sent as { mode: existing, app_id }', async () => {
    const calls = stubFetch({
      'POST /builder/sessions': () => json(201, session),
      'POST /builder/sessions/s1/messages': () => json(202, {}),
    })
    const { result } = setup()
    await act(async () => {
      await result.current.mutateAsync({ prompt, backend: { mode: 'existing', app_id: 'app-9' }, locale: 'en' })
    })
    expect(calls[0].body?.backend).toEqual({ mode: 'existing', app_id: 'app-9' })
  })

  test('when the message is refused it shows the API error, keeps the session and deletes nothing', async () => {
    const calls = stubFetch({
      'POST /builder/sessions': () => json(201, session),
      'POST /builder/sessions/s1/messages': () => json(429, { error: 'too many requests, try again later' }),
    })
    const { result } = setup()
    await act(async () => {
      await expect(
        result.current.mutateAsync({ prompt, backend: { mode: 'new' }, locale: 'en' }),
      ).rejects.toThrow('too many requests, try again later')
    })
    expect(toastError).toHaveBeenCalledWith('too many requests, try again later')
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)
  })

  test('the retry reuses the session: no second create, only the message again', async () => {
    let messageCalls = 0
    const calls = stubFetch({
      'POST /builder/sessions': () => json(201, session),
      'POST /builder/sessions/s1/messages': () =>
        ++messageCalls === 1 ? json(429, { error: 'slow down' }) : json(202, {}),
    })
    const { result } = setup()
    const input = { prompt, backend: { mode: 'new' as const }, locale: 'en' }
    await act(async () => {
      await expect(result.current.mutateAsync(input)).rejects.toThrow('slow down')
    })
    await act(async () => {
      await result.current.mutateAsync(input)
    })
    expect(calls.filter((c) => c.url.endsWith('/builder/sessions') && c.method === 'POST')).toHaveLength(1)
    expect(calls.filter((c) => c.url.endsWith('/messages'))).toHaveLength(2)
    await waitFor(() => expect(result.current.data).toEqual(session))
  })

  test('after a success the next project creates a new session', async () => {
    let created = 0
    const calls = stubFetch({
      'POST /builder/sessions': () => json(201, { id: `s${++created}` }),
      'POST /builder/sessions/s1/messages': () => json(202, {}),
      'POST /builder/sessions/s2/messages': () => json(202, {}),
    })
    const { result } = setup()
    const input = { prompt, backend: { mode: 'new' as const }, locale: 'en' }
    await act(async () => {
      await result.current.mutateAsync(input)
      await result.current.mutateAsync(input)
    })
    expect(calls.filter((c) => c.url.endsWith('/builder/sessions') && c.method === 'POST')).toHaveLength(2)
  })

  test('a different backend choice on the retry starts a new session', async () => {
    let created = 0
    let s1Messages = 0
    const calls = stubFetch({
      'POST /builder/sessions': () => json(201, { id: `s${++created}` }),
      'POST /builder/sessions/s1/messages': () => (++s1Messages, json(400, { error: 'message is empty' })),
      'POST /builder/sessions/s2/messages': () => json(202, {}),
    })
    const { result } = setup()
    await act(async () => {
      await expect(
        result.current.mutateAsync({ prompt, backend: { mode: 'new' }, locale: 'en' }),
      ).rejects.toThrow()
    })
    await act(async () => {
      await result.current.mutateAsync({ prompt, backend: { mode: 'existing', app_id: 'a1' }, locale: 'en' })
    })
    expect(created).toBe(2)
    await waitFor(() => expect(result.current.data).toEqual({ id: 's2' }))
    expect(s1Messages).toBe(1)
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)
  })

  // "No backend" and "New backend" are different projects: a retry after switching between them must not
  // reuse the session that was created for the other choice.
  test('switching between no backend and a new backend on the retry starts a new session', async () => {
    let created = 0
    const bodies: Array<Record<string, unknown> | null> = []
    const calls = stubFetch({
      'POST /builder/sessions': () => json(201, { id: `s${++created}` }),
      'POST /builder/sessions/s1/messages': () => json(400, { error: 'message is empty' }),
      'POST /builder/sessions/s2/messages': () => json(202, {}),
    })
    const { result } = setup()
    await act(async () => {
      await expect(
        result.current.mutateAsync({ prompt, backend: { mode: 'none' }, locale: 'en' }),
      ).rejects.toThrow()
    })
    await act(async () => {
      await result.current.mutateAsync({ prompt, backend: { mode: 'new' }, locale: 'en' })
    })
    for (const c of calls) if (c.url.endsWith('/builder/sessions') && c.method === 'POST') bodies.push(c.body)
    expect(created).toBe(2)
    expect(bodies.map((b) => (b?.backend as { mode: string }).mode)).toEqual(['none', 'new'])
    await waitFor(() => expect(result.current.data).toEqual({ id: 's2' }))
  })

  test('a failed create shows the error and posts no message', async () => {
    const calls = stubFetch({
      'POST /builder/sessions': () => json(429, { error: 'builder rate limit reached' }),
    })
    const { result } = setup()
    await act(async () => {
      await expect(
        result.current.mutateAsync({ prompt, backend: { mode: 'new' }, locale: 'en' }),
      ).rejects.toThrow('builder rate limit reached')
    })
    expect(toastError).toHaveBeenCalledWith('builder rate limit reached')
    expect(calls).toHaveLength(1)
  })

  test('refreshes the session and app lists when it settles', async () => {
    stubFetch({
      'POST /builder/sessions': () => json(201, session),
      'POST /builder/sessions/s1/messages': () => json(202, {}),
    })
    const { result, qc } = setup()
    const spy = vi.spyOn(qc, 'invalidateQueries')
    await act(async () => {
      await result.current.mutateAsync({ prompt, backend: { mode: 'new' }, locale: 'en' })
    })
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(2))
    const keys = spy.mock.calls.map((c) => (c[0] as { queryKey: unknown[] }).queryKey)
    expect(keys).toContainEqual(['builder', 'sessions'])
    expect(keys).toContainEqual(['apps'])
  })
})
