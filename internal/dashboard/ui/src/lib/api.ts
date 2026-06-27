import {
  useQuery,
  useMutation,
  useQueryClient,
  UseQueryResult,
  UseMutationResult,
} from '@tanstack/react-query'

// ── Types ──────────────────────────────────────────────────────────────────────

export interface ColumnDef {
  name: string
  type: string
  required: boolean
  default: string
  unique: boolean
}

export interface TableDef {
  id?: string
  name: string
  rls: string
  columns: ColumnDef[]
}

export interface AppDef {
  id: string
  name: string
  jwt_secret: string
  auth_email_enabled: boolean
  owner_id: string
  created_at: string
  tables: TableDef[]
}

export interface CreateAppInput {
  name: string
  auth_email_enabled: boolean
  tables: TableDef[]
}

// ── Fetchers ───────────────────────────────────────────────────────────────────

async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, { credentials: 'include', ...init })
  if (!res.ok) {
    let message = `HTTP ${res.status}`
    try {
      const body = await res.json()
      message = body.error ?? body.message ?? message
    } catch {
      // ignore parse errors
    }
    throw new Error(message)
  }
  if (res.status === 204) return undefined as unknown as T
  return res.json() as Promise<T>
}

// ── Hooks ──────────────────────────────────────────────────────────────────────

export function useApps(): UseQueryResult<AppDef[]> {
  return useQuery({
    queryKey: ['apps'],
    queryFn: () => apiFetch<AppDef[]>('/dashboard/api/apps'),
  })
}

export function useApp(id: string): UseQueryResult<AppDef> {
  return useQuery({
    queryKey: ['apps', id],
    queryFn: () => apiFetch<AppDef>(`/dashboard/api/apps/${id}`),
    enabled: Boolean(id),
  })
}

export function useCreateApp(): UseMutationResult<AppDef, Error, CreateAppInput> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: CreateAppInput) =>
      apiFetch<AppDef>('/dashboard/api/apps', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['apps'] })
    },
  })
}

export function useUpdateApp(): UseMutationResult<AppDef, Error, { id: string } & CreateAppInput> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, ...input }) =>
      apiFetch<AppDef>(`/dashboard/api/apps/${id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['apps'] })
    },
  })
}

export function useDeleteApp(): UseMutationResult<void, Error, string> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      apiFetch<void>(`/dashboard/api/apps/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['apps'] })
    },
  })
}
