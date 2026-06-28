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

// ── Users ──────────────────────────────────────────────────────────────────────

export interface UserDef {
  id: string
  email: string
  role: string
  created_at: string
}

export interface CreateUserInput {
  email: string
  password: string
  role: string
}

export function useUsers(): UseQueryResult<UserDef[]> {
  return useQuery({
    queryKey: ['users'],
    queryFn: () => apiFetch<UserDef[]>('/dashboard/api/users'),
  })
}

export function useCreateUser(): UseMutationResult<
  { id: string; email: string; role: string },
  Error,
  CreateUserInput
> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input) =>
      apiFetch('/dashboard/api/users', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['users'] })
    },
  })
}

export function useDeleteUser(): UseMutationResult<void, Error, string> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      apiFetch<void>(`/dashboard/api/users/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['users'] })
    },
  })
}

// ── Bootstrap / Config ──────────────────────────────────────────────────────────

export interface BootstrapStatus {
  bootstrapped: boolean
}

export interface BrandConfig {
  theme: string
  company_name: string
  logo_url: string
}

export function useBootstrapStatus(): UseQueryResult<BootstrapStatus> {
  return useQuery({
    queryKey: ['bootstrap-status'],
    queryFn: () => apiFetch<BootstrapStatus>('/dashboard/api/bootstrap/status'),
    staleTime: Infinity,
    retry: false,
  })
}

export function useBootstrap(): UseMutationResult<
  { message: string; email: string },
  Error,
  { secret: string; email: string; password: string }
> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input) =>
      apiFetch('/dashboard/api/bootstrap', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['bootstrap-status'] })
    },
  })
}

export function useBrandConfig(): UseQueryResult<BrandConfig> {
  return useQuery({
    queryKey: ['brand-config'],
    queryFn: () => apiFetch<BrandConfig>('/dashboard/api/config'),
    staleTime: 30000,
  })
}

// ── Data Browser ──────────────────────────────────────────────────────────────

export interface DataBrowserColumn {
  name: string
  type: string
}

export interface DataBrowserTable {
  name: string
  columns: DataBrowserColumn[]
}

export interface DataBrowserApp {
  name: string
  tables: DataBrowserTable[]
}

export interface QueryResult {
  data: Record<string, unknown>[]
  count: number
  limit: number
  offset: number
}

export function useDataBrowserApps(): UseQueryResult<DataBrowserApp[]> {
  return useQuery({
    queryKey: ['data-browser-apps'],
    queryFn: () => apiFetch<DataBrowserApp[]>('/dashboard/api/data-browser/apps'),
  })
}

export function useDataBrowserQuery(
  app: string,
  table: string,
  limit: number,
  offset: number,
  order?: string,
): UseQueryResult<QueryResult> {
  return useQuery({
    queryKey: ['data-browser-query', app, table, limit, offset, order],
    queryFn: () => {
      const params = new URLSearchParams()
      params.set('app', app)
      params.set('table', table)
      params.set('limit', String(limit))
      params.set('offset', String(offset))
      if (order) params.set('order', order)
      return apiFetch<QueryResult>(`/dashboard/api/data-browser/query?${params}`)
    },
    enabled: Boolean(app) && Boolean(table),
  })
}

// ── Logs ──────────────────────────────────────────────────────────────────────

export interface LogEntry {
  timestamp: string
  app: string
  method: string
  path: string
  status: number
  latency_ms: number
  user_agent?: string
}

export interface LogMetrics {
  total_requests: number
  requests_per_app: Record<string, number>
  avg_latency_ms: number
  errors_4xx: number
  errors_5xx: number
  method_breakdown: Record<string, number>
}

export function useLogs(appFilter?: string): UseQueryResult<LogEntry[]> {
  return useQuery({
    queryKey: ['logs', appFilter],
    queryFn: () => {
      const params = new URLSearchParams()
      params.set('limit', '200')
      if (appFilter) params.set('app', appFilter)
      return apiFetch<LogEntry[]>(`/dashboard/api/logs?${params}`)
    },
    refetchInterval: 3000,
  })
}

export function useLogMetrics(): UseQueryResult<LogMetrics> {
  return useQuery({
    queryKey: ['logs-metrics'],
    queryFn: () => apiFetch<LogMetrics>('/dashboard/api/logs/metrics'),
    refetchInterval: 5000,
  })
}

export function useUpdateBrandConfig(): UseMutationResult<
  BrandConfig,
  Error,
  { theme?: string; company_name?: string; logo_url?: string }
> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input) =>
      apiFetch<BrandConfig>('/dashboard/api/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      }),
    onSuccess: (data) => {
      qc.setQueryData(['brand-config'], data)
    },
  })
}
