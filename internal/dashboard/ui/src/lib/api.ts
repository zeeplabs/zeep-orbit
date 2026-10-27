import { useEffect, useRef } from 'react'
import {
  useQuery,
  useMutation,
  useQueryClient,
  UseQueryResult,
  UseMutationResult,
} from '@tanstack/react-query'
import { toast } from 'sonner'


export interface ReferenceDef {
  table: string
  column: string
  on_delete?: '' | 'cascade' | 'restrict' | 'set_null' | 'no_action'
}

export interface ColumnDef {
  name: string
  type: string
  required: boolean
  default: string
  default_is_expression?: boolean
  unique: boolean
  references?: ReferenceDef | null
  // allowed_values is the fixed set of values a "enum"-type column accepts
  // (column-enum-type T1's Go counterpart: config.ColumnConfig.AllowedValues).
  // Only meaningful when type === "enum".
  allowed_values?: string[]
}

export interface IndexDef {
  name: string
  columns: string[]
  unique?: boolean
}

export interface TableDef {
  id?: string
  name: string
  rls: string
  columns: ColumnDef[]
  indexes?: IndexDef[]
}

export interface AppDef {
  id: string
  name: string
  jwt_secret: string
  auth_email_enabled: boolean
  enduser_roles_config: string[]
  owner_id: string
  owner_email?: string
  owner_name?: string
  created_at: string
  tables: TableDef[]
  can_write?: boolean
}

export interface CreateAppInput {
  name: string
  auth_email_enabled: boolean
}


export interface PublicConfig {
  theme: string
  google_oauth_enabled: boolean
}

export function usePublicConfig() {
  return useQuery({
    queryKey: ['public-config'],
    queryFn: () =>
      fetch('/dashboard/api/config').then((r) => r.json()) as Promise<PublicConfig>,
    staleTime: 60000,
  })
}

export interface SystemConfig {
  soft_delete_enabled: boolean
  max_csv_export_rows: number
  statement_timeout_ms: number
  require_rls_default: boolean
  retention_days: number
}

export function useSystemConfig(): UseQueryResult<SystemConfig> {
  return useQuery({
    queryKey: ['system-config'],
    queryFn: () => apiFetch<SystemConfig>('/dashboard/api/config/system'),
    staleTime: 60000,
  })
}

let redirectingToLogin = false

// An expired or revoked session answers 401 on every API call, but nothing told the
// user: pages just showed a generic error. Send them to /login and bring them back to
// the same screen afterwards. A full navigation (not the router) is deliberate: it
// drops the query cache with the dead session, and it cannot be held back by the
// unsaved-changes blocker, which would otherwise trap a user whose edits can't be saved.
export function redirectToLoginOnUnauthorized(): void {
  const { pathname, search } = window.location
  if (redirectingToLogin || pathname.startsWith('/dashboard/login')) return
  redirectingToLogin = true
  const returnTo = encodeURIComponent(pathname + search)
  window.location.assign(`/dashboard/login?return_to=${returnTo}`)
  // With unsaved edits the browser asks before leaving. If the user stays (to copy what
  // they typed), the page is still here: let the next 401 try again instead of never.
  setTimeout(() => {
    redirectingToLogin = false
  }, 2000)
}

// Drop-in `fetch` that sends the session on 401 to the login screen. For call sites that
// need the raw Response (custom error handling); everything else should use apiFetch.
export async function authFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const res = await fetch(input, init)
  if (res.status === 401 && (await isSessionExpired(res))) redirectToLoginOnUnauthorized()
  return res
}

// Not every 401 means the session is gone: changing a password with a wrong current
// password also answers 401 ("current password is incorrect"). Only the auth middleware's
// body, {"error":"unauthorized"}, is a dead session.
async function isSessionExpired(res: Response): Promise<boolean> {
  try {
    const body = await res.clone().json()
    return body?.error === 'unauthorized'
  } catch {
    return false
  }
}

// ApiError is what apiFetch throws for a non-2xx answer. It is still an Error whose
// message is the server's English `error` string (so `toast.error(e.message)` keeps
// working), and it also carries the HTTP status and the machine-readable `reason` some
// routes add next to `error` (e.g. the builder publish 409s), so UIs can branch on a code
// instead of parsing text.
export class ApiError extends Error {
  readonly status: number
  readonly reason: string | undefined

  constructor(message: string, status: number, reason?: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.reason = reason
  }
}

async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await authFetch(path, { credentials: 'include', ...init })
  if (!res.ok) {
    let message = `HTTP ${res.status}`
    let reason: string | undefined
    try {
      const body = await res.json()
      message = body.error ?? body.message ?? message
      if (typeof body.reason === 'string') reason = body.reason
    } catch {
    }
    throw new ApiError(message, res.status, reason)
  }
  if (res.status === 204) return undefined as unknown as T
  return res.json() as Promise<T>
}


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

export function useCreateAppTable(appId: string): UseMutationResult<TableDef, Error, TableDef> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (table: TableDef) =>
      apiFetch<TableDef>(`/dashboard/api/apps/${appId}/tables`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(table),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['apps'] })
    },
  })
}

export function useUpdateAppTable(
  appId: string,
): UseMutationResult<TableDef, Error, { tableId: string; rls: string; columns: ColumnDef[]; indexes?: IndexDef[] }> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ tableId, ...input }) =>
      apiFetch<TableDef>(`/dashboard/api/apps/${appId}/tables/${tableId}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['apps'] })
    },
  })
}

// useUpdateColumnEnumValues calls the dedicated PATCH endpoint for
// widening/narrowing an existing enum column's allowed values — the generic
// useUpdateAppTable's PUT path can't apply this change (the REST handler
// rejects an AllowedValues change on an existing column, see AGENTS.md's
// column-enum-type notes).
export function useUpdateColumnEnumValues(
  appId: string,
  tableId: string,
): UseMutationResult<void, Error, { columnName: string; values: string[] }> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ columnName, values }) =>
      apiFetch<void>(
        `/dashboard/api/apps/${appId}/tables/${tableId}/columns/${encodeURIComponent(columnName)}/enum-values`,
        {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ allowed_values: values }),
        },
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['apps'] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export function useDeleteAppTable(appId: string): UseMutationResult<void, Error, string> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (tableId: string) =>
      apiFetch<void>(`/dashboard/api/apps/${appId}/tables/${tableId}`, { method: 'DELETE' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['apps'] })
    },
  })
}


export interface PolicyClause {
  column: string
  operator: string
  value_source?: string
  value?: string
  logic?: string
}

export interface PolicyDef {
  name: string
  action: string
  roles: string[]
  clauses: PolicyClause[]
}

export interface TablePolicyRow {
  id: string
  table_name: string
  action: string
  roles: string[]
  clauses: PolicyClause[]
  pg_policy_name: string
  created_at: string
  created_by: string
  updated_at: string | null
  updated_by: string | null
}

export function useTablePolicies(appId: string, table: string): UseQueryResult<TablePolicyRow[]> {
  return useQuery({
    queryKey: ['table-policies', appId, table],
    queryFn: () =>
      apiFetch<TablePolicyRow[]>(`/dashboard/api/apps/${appId}/tables/${table}/policies`),
    enabled: Boolean(appId) && Boolean(table),
  })
}

export function useCreateTablePolicy(
  appId: string,
  table: string,
): UseMutationResult<TablePolicyRow, Error, PolicyDef> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (def: PolicyDef) =>
      apiFetch<TablePolicyRow>(`/dashboard/api/apps/${appId}/tables/${table}/policies`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(def),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['table-policies', appId, table] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export function useUpdateTablePolicy(
  appId: string,
  table: string,
): UseMutationResult<TablePolicyRow, Error, { policyId: string; def: PolicyDef }> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ policyId, def }) =>
      apiFetch<TablePolicyRow>(
        `/dashboard/api/apps/${appId}/tables/${table}/policies/${policyId}`,
        {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(def),
        },
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['table-policies', appId, table] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export function useDeleteTablePolicy(
  appId: string,
  table: string,
): UseMutationResult<{ message: string }, Error, string> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (policyId: string) =>
      apiFetch<{ message: string }>(
        `/dashboard/api/apps/${appId}/tables/${table}/policies/${policyId}`,
        { method: 'DELETE' },
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['table-policies', appId, table] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}


export interface UserDef {
  id: string
  email: string
  name?: string
  role: string
  created_at: string
  /** Derived sign-in method: "google" when the account is linked to Google, "email" otherwise. */
  sign_in?: string
}

export interface CreateUserInput {
  email: string
  name?: string
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


export interface AppUserSummary {
  id: string
  email: string
  name: string | null
  phone: string | null
  avatar_url: string | null
  provider: string
  role: string
  active: boolean
  last_sign_in_at: string | null
  created_at: string
}

export interface AppUserProviderCount {
  provider: string
  count: number
}

export interface AppUserListResponse {
  data: AppUserSummary[]
  total: number
  limit: number
  offset: number
  providerCounts: AppUserProviderCount[]
}

export function useAppUsers(
  appId: string,
  search?: string,
  limit?: number,
  offset?: number,
): UseQueryResult<AppUserListResponse> {
  return useQuery({
    queryKey: ['app-users', appId, search, limit, offset],
    queryFn: () => {
      const params = new URLSearchParams()
      if (search) params.set('search', search)
      if (limit) params.set('limit', String(limit))
      if (offset) params.set('offset', String(offset))
      const qs = params.toString()
      return apiFetch<AppUserListResponse>(
        `/dashboard/api/apps/${appId}/users${qs ? '?' + qs : ''}`,
      )
    },
    enabled: Boolean(appId),
  })
}

export function useDeactivateAppUser(): UseMutationResult<
  { message: string },
  Error,
  { appId: string; userId: string }
> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ appId, userId }) =>
      apiFetch(`/dashboard/api/apps/${appId}/users/${userId}/deactivate`, {
        method: 'PUT',
      }),
    onSuccess: (_data, variables) => {
      qc.invalidateQueries({ queryKey: ['app-users', variables.appId] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export function useActivateAppUser(): UseMutationResult<
  { message: string },
  Error,
  { appId: string; userId: string }
> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ appId, userId }) =>
      apiFetch(`/dashboard/api/apps/${appId}/users/${userId}/activate`, {
        method: 'PUT',
      }),
    onSuccess: (_data, variables) => {
      qc.invalidateQueries({ queryKey: ['app-users', variables.appId] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export function useUpdateAppUser(): UseMutationResult<
  { message: string },
  Error,
  { appId: string; userId: string; email: string; phone: string; role: string }
> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ appId, userId, email, phone, role }) =>
      apiFetch(`/dashboard/api/apps/${appId}/users/${userId}`, {
        method: 'PUT',
        body: JSON.stringify({ email, phone, role }),
      }),
    onSuccess: (_data, variables) => {
      qc.invalidateQueries({ queryKey: ['app-users', variables.appId] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export function useUpdateAppEnduserRoles(): UseMutationResult<
  { roles: string[] },
  Error,
  { id: string; roles: string[] }
> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, roles }) =>
      apiFetch<{ roles: string[] }>(`/dashboard/api/apps/${id}/roles`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ roles }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['apps'] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export function useResetAppUserSessions(): UseMutationResult<
  { message: string },
  Error,
  { appId: string; userId: string }
> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ appId, userId }) =>
      apiFetch(`/dashboard/api/apps/${appId}/users/${userId}/reset-sessions`, {
        method: 'POST',
      }),
    onSuccess: (_data, variables) => {
      qc.invalidateQueries({ queryKey: ['app-users', variables.appId] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}


export interface BootstrapStatus {
  bootstrapped: boolean
}

export interface BrandConfig {
  theme: string
  company_name: string
  logo_url: string
  google_oauth_enabled: boolean
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
  { secret: string; email: string; name: string; password: string }
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


export interface DataBrowserColumn {
  name: string
  type: string
  allowed_values?: string[]
}

export interface DataBrowserTable {
  name: string
  columns: DataBrowserColumn[]
  count: number
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
  filters?: Record<string, string>,
): UseQueryResult<QueryResult> {
  return useQuery({
    queryKey: ['data-browser-query', app, table, limit, offset, order, filters],
    queryFn: () => {
      const params = new URLSearchParams()
      params.set('app', app)
      params.set('table', table)
      params.set('limit', String(limit))
      params.set('offset', String(offset))
      if (order) params.set('order', order)
      if (filters) {
        for (const [col, val] of Object.entries(filters)) {
          if (val) params.set(col, val)
        }
      }
      return apiFetch<QueryResult>(`/dashboard/api/data-browser/query?${params}`)
    },
    enabled: Boolean(app) && Boolean(table),
  })
}

export async function exportDataBrowserCSV(
  app: string,
  table: string,
  filters?: Record<string, string>,
): Promise<void> {
  const params = new URLSearchParams()
  params.set('app', app)
  params.set('table', table)
  if (filters) {
    for (const [col, val] of Object.entries(filters)) {
      if (val) params.set(col, val)
    }
  }
  const res = await authFetch(`/dashboard/api/data-browser/export?${params}`, {
    credentials: 'include',
  })
  if (!res.ok) throw new Error('Export failed')
  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${app}_${table}.csv`
  a.click()
  URL.revokeObjectURL(url)
}

export interface MutationRowInput {
  app: string
  table: string
  data: Record<string, unknown>
}

export interface UpdateRowInput extends MutationRowInput {
  id: string
}

export interface RowResponse {
  data: Record<string, unknown>
}

export function useCreateDataBrowserRow(): UseMutationResult<RowResponse, Error, MutationRowInput> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: MutationRowInput) =>
      apiFetch<RowResponse>('/dashboard/api/data-browser/row', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      }),
    onSuccess: (_data, variables) => {
      qc.invalidateQueries({ queryKey: ['data-browser-query', variables.app, variables.table] })
    },
  })
}

export function useUpdateDataBrowserRow(): UseMutationResult<RowResponse, Error, UpdateRowInput> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: UpdateRowInput) =>
      apiFetch<RowResponse>('/dashboard/api/data-browser/row', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      }),
    onSuccess: (_data, variables) => {
      qc.invalidateQueries({ queryKey: ['data-browser-query', variables.app, variables.table] })
    },
  })
}

export function useDeleteDataBrowserRow(): UseMutationResult<void, Error, { app: string; table: string; id: string }> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ app, table, id }) =>
      apiFetch<void>(`/dashboard/api/data-browser/row?app=${encodeURIComponent(app)}&table=${encodeURIComponent(table)}&id=${encodeURIComponent(id)}`, {
        method: 'DELETE',
      }),
    onSuccess: (_data, variables) => {
      qc.invalidateQueries({ queryKey: ['data-browser-query', variables.app, variables.table] })
    },
  })
}


export interface LogEntry {
  timestamp: string
  app: string
  method: string
  path: string
  query?: string
  status: number
  latency_ms: number
  user_agent?: string
  remote_addr?: string
  req_body?: string
  res_body?: string
  content_type?: string
}

export interface LogMetrics {
  total_requests: number
  requests_per_app: Record<string, number>
  avg_latency_ms: number
  errors_4xx: number
  errors_5xx: number
  method_breakdown: Record<string, number>
}

export function useLogs(appFilter?: string, autoRefresh = true): UseQueryResult<LogEntry[]> {
  return useQuery({
    queryKey: ['logs', appFilter],
    queryFn: () => {
      const params = new URLSearchParams()
      params.set('limit', '200')
      if (appFilter) params.set('app', appFilter)
      return apiFetch<LogEntry[]>(`/dashboard/api/logs?${params}`)
    },
    refetchInterval: autoRefresh ? 10000 : false,
  })
}

export function useLogMetrics(autoRefresh = true): UseQueryResult<LogMetrics> {
  return useQuery({
    queryKey: ['logs-metrics'],
    queryFn: () => apiFetch<LogMetrics>('/dashboard/api/logs/metrics'),
    refetchInterval: autoRefresh ? 10000 : false,
  })
}


export interface ChangeMyPasswordInput {
  current_password: string
  new_password: string
  confirm_password: string
}

export interface ChangeUserPasswordInput {
  new_password: string
  confirm_password: string
}

export interface AppToken {
  id: string
  app_id: string
  name: string
  jti: string
  expires_at: string | null
  revoked_at: string | null
  last_used_at: string | null
  created_at: string
}

export interface CreateAppTokenInput {
  name: string
  expiration: '7d' | '30d' | '365d' | 'never'
}

export interface CreateAppTokenResponse {
  token: string
  row: AppToken
}

export function useAppTokens(appId: string): UseQueryResult<AppToken[]> {
  return useQuery({
    queryKey: ['app-tokens', appId],
    queryFn: () => apiFetch<AppToken[]>(`/dashboard/api/apps/${appId}/tokens`),
    enabled: Boolean(appId),
  })
}

export function useCreateAppToken(appId: string): UseMutationResult<CreateAppTokenResponse, Error, CreateAppTokenInput> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input) =>
      apiFetch<CreateAppTokenResponse>(`/dashboard/api/apps/${appId}/tokens`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['app-tokens', appId] })
    },
  })
}

export function useRevokeAppToken(appId: string): UseMutationResult<{ message: string }, Error, string> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (tokenId) =>
      apiFetch(`/dashboard/api/apps/${appId}/tokens/${tokenId}/revoke`, { method: 'POST' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['app-tokens', appId] })
    },
  })
}

export function useRegenerateAppSecret(appId: string): UseMutationResult<{ jwt_secret: string }, Error, void> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () =>
      apiFetch<{ jwt_secret: string }>(`/dashboard/api/apps/${appId}/regenerate-secret`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirm: true }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['apps'] })
      qc.invalidateQueries({ queryKey: ['app-tokens', appId] })
    },
  })
}

export function useAppSecret(appId: string): UseQueryResult<{ jwt_secret: string }> {
  return useQuery({
    queryKey: ['app-secret', appId],
    queryFn: () => apiFetch<{ jwt_secret: string }>(`/dashboard/api/apps/${appId}/secret`),
    enabled: Boolean(appId),
  })
}

export function useChangeMyPassword(): UseMutationResult<
  { message: string },
  Error,
  ChangeMyPasswordInput
> {
  return useMutation({
    mutationFn: (input) =>
      apiFetch('/dashboard/api/me/password', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      }),
  })
}

export function useChangeUserPassword(): UseMutationResult<
  { message: string },
  Error,
  { userId: string } & ChangeUserPasswordInput
> {
  return useMutation({
    mutationFn: ({ userId, ...input }) =>
      apiFetch(`/dashboard/api/users/${userId}/password`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      }),
  })
}

// PersonalAccessToken mirrors internal/dashboard/pat_store.go's PATRow —
// token_hash is intentionally never a field here (mcp-server spec P1 AC1).
export interface PersonalAccessToken {
  id: string
  user_id: string
  name: string
  kind: 'manual' | 'ephemeral' | 'oauth'
  oauth_client_id?: string
  expires_at?: string
  revoked_at?: string
  last_used_at?: string
  created_at: string
}

// CreatePATResult is PersonalAccessToken plus the plaintext token, present
// only on the create response (shown once, never retrievable again).
export interface CreatePATResult extends PersonalAccessToken {
  token: string
}

export function usePATs(): UseQueryResult<PersonalAccessToken[]> {
  return useQuery({
    queryKey: ['pats'],
    queryFn: () => apiFetch<PersonalAccessToken[]>('/dashboard/api/me/pats'),
  })
}

export function useCreatePAT(): UseMutationResult<CreatePATResult, Error, string> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (name: string) =>
      apiFetch<CreatePATResult>('/dashboard/api/me/pats', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['pats'] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export function useRevokePAT(): UseMutationResult<{ message: string }, Error, string> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (patId: string) =>
      apiFetch<{ message: string }>(`/dashboard/api/me/pats/${patId}`, {
        method: 'DELETE',
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['pats'] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

// RegisteredOAuthClient mirrors internal/dashboard/oauth_client_store.go's
// OAuthClient — a dynamically self-registered MCP client (RFC 7591).
export interface RegisteredOAuthClient {
  id: string
  name: string
  redirect_uris: string[]
  created_at: string
}

export function useOAuthClients(): UseQueryResult<RegisteredOAuthClient[]> {
  return useQuery({
    queryKey: ['oauth-clients'],
    queryFn: () => apiFetch<RegisteredOAuthClient[]>('/dashboard/api/oauth-clients'),
  })
}

export function useDeleteOAuthClient(): UseMutationResult<{ message: string }, Error, string> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (clientId: string) =>
      apiFetch<{ message: string }>(`/dashboard/api/oauth-clients/${clientId}`, {
        method: 'DELETE',
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['oauth-clients'] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export interface AuditEntry {
  id: string
  user_id: string
  user_email: string
  action: string
  resource_type: string
  resource_id?: string
  resource_name?: string
  metadata?: Record<string, unknown>
  ip_address?: string
  created_at: string
}

export interface AuditLogResponse {
  data: AuditEntry[]
  total: number
  limit: number
  offset: number
}

export function useAuditLog(
  limit = 50,
  offset = 0,
  action?: string,
  category?: string,
  userEmail?: string,
): UseQueryResult<AuditLogResponse> {
  return useQuery({
    queryKey: ['audit-log', limit, offset, action, category, userEmail],
    queryFn: () => {
      const params = new URLSearchParams()
      params.set('limit', String(limit))
      params.set('offset', String(offset))
      if (action) params.set('action', action)
      if (category) params.set('category', category)
      if (userEmail) params.set('user_email', userEmail)
      return apiFetch<AuditLogResponse>(`/dashboard/api/audit-log?${params}`)
    },
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


// ---- App members (rbac-per-app T-06 + T-09) ----

export type AppRole = 'admin' | 'editor' | 'viewer'

export interface AppMember {
  id: string
  user_id: string
  role: AppRole
  created_at: string
}

export interface AppMemberListResponse {
  members: AppMember[]
}

export type AppAxis = 'backend' | 'frontend'

function appMembersPath(axis: AppAxis, appId: string): string {
  const base = axis === 'backend' ? 'apps' : 'frontend-apps'
  return `/dashboard/api/${base}/${appId}/members`
}

/**
 * Returns the membership list for an app. Per T-06 AC-6, the backend
 * returns 403 for editor/viewer/non-member on every /members endpoint
 * (including GET), so a non-admin caller will see `error` set rather
 * than an empty list — the component should render a "no access" state.
 */
export function useAppMembers(
  appId: string,
  axis: AppAxis,
): UseQueryResult<AppMemberListResponse> {
  return useQuery({
    queryKey: ['app-members', axis, appId],
    queryFn: () => apiFetch<AppMemberListResponse>(appMembersPath(axis, appId)),
    enabled: Boolean(appId),
  })
}

export interface AddAppMemberInput {
  user_id: string
  role: AppRole
}

export function useAddAppMember(
  appId: string,
  axis: AppAxis,
): UseMutationResult<AppMember, Error, AddAppMemberInput> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input) =>
      apiFetch<AppMember>(appMembersPath(axis, appId), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['app-members', axis, appId] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export interface UpdateAppMemberInput {
  user_id: string
  role: AppRole
}

export function useUpdateAppMember(
  appId: string,
  axis: AppAxis,
): UseMutationResult<AppMember, Error, UpdateAppMemberInput> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ user_id, role }) =>
      apiFetch<AppMember>(`${appMembersPath(axis, appId)}/${user_id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ role }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['app-members', axis, appId] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export function useRemoveAppMember(
  appId: string,
  axis: AppAxis,
): UseMutationResult<void, Error, string> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (user_id: string) =>
      apiFetch<void>(`${appMembersPath(axis, appId)}/${user_id}`, {
        method: 'DELETE',
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['app-members', axis, appId] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}


// ----------------------------------------------------------------------------
// Inbound webhooks (T13) — types + hooks match the dashboard handler
// responses from T9-T11 (internal/dashboard/webhooks_handler.go) exactly.
// ----------------------------------------------------------------------------

export interface WebhookSubscription {
  id: string
  app_id: string
  name: string
  method: 'GET' | 'POST' | 'PUT' | 'PATCH'
  /**
   * Plaintext token, decrypted server-side on every response (the secret is
   * stored encrypted, not hashed, precisely so the dashboard can always
   * render the full callback URL). Empty for a webhook created before the
   * hash -> encryption migration — rotate it once to get a working token.
   */
  token: string
  event_type_path: string
  event_id_path: string | null
  status: 'capture' | 'active'
  captured_sample: Record<string, unknown> | null
  deleted_at: string | null
  created_by: string
  created_at: string
  updated_at: string
}

export interface CreateWebhookInput {
  name: string
  method: 'GET' | 'POST' | 'PUT' | 'PATCH'
  event_type_path: string
  event_id_path?: string
}

export interface UpdateWebhookInput {
  name: string
  method: 'GET' | 'POST' | 'PUT' | 'PATCH'
  event_type_path: string
  event_id_path?: string
}

export interface WebhookFieldMapping {
  source_path: string
  column: string
}

export interface WebhookEventMapping {
  id: string
  webhook_id: string
  event_type_value: string
  action: 'insert' | 'update' | 'delete'
  target_table: string
  match_key_column: string | null
  field_mappings: WebhookFieldMapping[]
  created_at: string
  updated_at: string
}

export interface SaveEventMappingInput {
  event_type_value: string
  action: 'insert' | 'update' | 'delete'
  target_table: string
  match_key_column?: string
  field_mappings: WebhookFieldMapping[]
}

export interface WebhookDelivery {
  id: string
  webhook_id: string
  received_at: string
  http_status: number
  outcome:
    | 'captured'
    | 'inserted'
    | 'updated'
    | 'deleted'
    | 'unmapped'
    | 'duplicate_skipped'
    | 'row_not_found'
    | 'invalid_token'
    | 'malformed'
    | 'write_error'
    | 'verification_challenge'
    | 'ambiguous_match'
  event_type_value: string | null
  event_id: string | null
  raw_payload: Record<string, unknown>
  target_row_id: string | null
  error_detail: string | null
}

export function useWebhooks(appId: string): UseQueryResult<WebhookSubscription[]> {
  return useQuery({
    queryKey: ['webhooks', appId],
    queryFn: () => apiFetch<WebhookSubscription[]>(`/dashboard/api/apps/${appId}/webhooks`),
    enabled: Boolean(appId),
    // Poll while any webhook is still waiting for its first sample — the
    // capture happens from an external provider's own call, outside any
    // mutation this dashboard tab knows about, so nothing else invalidates
    // this query when the sample actually arrives.
    refetchInterval: (query) => {
      const stillCapturing = query.state.data?.some(
        (wh) => wh.status === 'capture' && !wh.captured_sample,
      )
      return stillCapturing ? 3000 : false
    },
  })
}

export function useCreateWebhook(
  appId: string,
): UseMutationResult<WebhookSubscription, Error, CreateWebhookInput> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: CreateWebhookInput) =>
      apiFetch<WebhookSubscription>(`/dashboard/api/apps/${appId}/webhooks`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['webhooks', appId] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export function useRotateWebhookToken(
  appId: string,
): UseMutationResult<WebhookSubscription, Error, string> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (webhookId: string) =>
      apiFetch<WebhookSubscription>(
        `/dashboard/api/apps/${appId}/webhooks/${webhookId}/rotate-token`,
        { method: 'POST' },
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['webhooks', appId] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export function useUpdateWebhook(
  appId: string,
): UseMutationResult<WebhookSubscription, Error, { webhookId: string } & UpdateWebhookInput> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ webhookId, ...input }) =>
      apiFetch<WebhookSubscription>(`/dashboard/api/apps/${appId}/webhooks/${webhookId}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['webhooks', appId] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export function useDeleteWebhook(
  appId: string,
): UseMutationResult<{ message: string }, Error, string> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (webhookId: string) =>
      apiFetch<{ message: string }>(`/dashboard/api/apps/${appId}/webhooks/${webhookId}`, {
        method: 'DELETE',
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['webhooks', appId] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export function useEventMappings(
  appId: string,
  webhookId: string,
): UseQueryResult<WebhookEventMapping[]> {
  return useQuery({
    queryKey: ['webhook-mappings', appId, webhookId],
    queryFn: () =>
      apiFetch<WebhookEventMapping[]>(
        `/dashboard/api/apps/${appId}/webhooks/${webhookId}/mappings`,
      ),
    enabled: Boolean(appId) && Boolean(webhookId),
  })
}

export function useSaveEventMapping(
  appId: string,
  webhookId: string,
): UseMutationResult<WebhookEventMapping, Error, SaveEventMappingInput> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: SaveEventMappingInput) =>
      apiFetch<WebhookEventMapping>(
        `/dashboard/api/apps/${appId}/webhooks/${webhookId}/mappings`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(input),
        },
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['webhook-mappings', appId, webhookId] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export function useDeleteEventMapping(
  appId: string,
  webhookId: string,
): UseMutationResult<{ message: string }, Error, string> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (mappingId: string) =>
      apiFetch<{ message: string }>(
        `/dashboard/api/apps/${appId}/webhooks/${webhookId}/mappings/${mappingId}`,
        { method: 'DELETE' },
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['webhook-mappings', appId, webhookId] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export function useActivateWebhook(
  appId: string,
): UseMutationResult<{ status: string }, Error, string> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (webhookId: string) =>
      apiFetch<{ status: string }>(
        `/dashboard/api/apps/${appId}/webhooks/${webhookId}/activate`,
        { method: 'POST' },
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['webhooks', appId] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export function useWebhookDeliveries(
  appId: string,
  webhookId: string,
): UseQueryResult<WebhookDelivery[]> {
  return useQuery({
    queryKey: ['webhook-deliveries', appId, webhookId],
    queryFn: () =>
      apiFetch<WebhookDelivery[]>(
        `/dashboard/api/apps/${appId}/webhooks/${webhookId}/deliveries`,
      ),
    enabled: Boolean(appId) && Boolean(webhookId),
  })
}

// ----------------------------------------------------------------------------
// Build with AI (ai-build-chat T12) — chat-driven app creation. Types mirror
// internal/dashboard/ai_build_chat_handlers.go's response shapes and
// internal/dashboard/ai/client.go's AppPlan exactly.
// ----------------------------------------------------------------------------

// Provider kinds the backend persists today (ai_provider_handlers.go aiProviderImplemented).
// gemini/claude are accepted names but answer {available: false} on GET and 501 on PUT.
export type AIProviderKind = 'openai' | 'openai_compatible'

// Mirrors AIProviderResponse (ai_providers_store.go). base_url is "" for 'openai' and for an
// unconfigured provider; it is never a secret (credentials in the URL are rejected). The API
// key is never returned, only has_key.
export interface AIProviderStatus {
  provider?: string
  has_key?: boolean
  model?: string
  enabled?: boolean
  base_url?: string
  updated_at?: string
  available?: boolean
}

// Body of PUT /dashboard/api/ai-providers/{provider} (aiProviderUpsertInput). Merge-on-absent-key:
//  - api_key: omit (or "") keeps the stored key; the key can only be replaced, not cleared.
//  - base_url: only for 'openai_compatible', where it is required. Omit keeps the stored one;
//    an explicit "" is rejected with 400 (the provider cannot work without it). For 'openai' send
//    nothing, or "" (a non-empty value is a 400).
//  - model is required (non-blank) and enabled is always sent.
export interface AIProviderUpsertInput {
  model: string
  enabled: boolean
  api_key?: string
  base_url?: string
}

export function useAIProviderStatus(provider = 'openai'): UseQueryResult<AIProviderStatus> {
  return useQuery({
    queryKey: ['ai-provider-status', provider],
    queryFn: () => apiFetch<AIProviderStatus>(`/dashboard/api/ai-providers/${provider}`),
    staleTime: 30000,
  })
}

export interface BuildChatPlanColumn {
  name: string
  type: string
  allowed_values?: string[]
}

export interface BuildChatPlanTable {
  name: string
  columns: BuildChatPlanColumn[]
}

export interface BuildChatPlan {
  name: string
  tables: BuildChatPlanTable[]
  auth: boolean
}

export interface BuildChatMessage {
  id: string
  session_id: string
  role: 'user' | 'assistant'
  content: string
  plan?: BuildChatPlan
  created_at: string
}

export interface BuildChatSession {
  id: string
  owner_user_id: string
  status: 'in_progress' | 'completed' | 'abandoned'
  created_app_id?: string
  created_at: string
  updated_at: string
}

export interface BuildChatSessionResponse {
  session: BuildChatSession
  messages: BuildChatMessage[]
}

// useBuildChatSession resumes the caller's in_progress session (or creates
// one) — `enabled` gates the fetch so the drawer only opens this request
// once mounted with `open: true`, not on every AppsPage render.
export function useBuildChatSession(enabled: boolean): UseQueryResult<BuildChatSessionResponse> {
  return useQuery({
    queryKey: ['build-chat-session'],
    queryFn: () => apiFetch<BuildChatSessionResponse>('/dashboard/api/ai/build-chat/session'),
    enabled,
    refetchOnWindowFocus: false,
  })
}

export interface BuildChatTurnResponse {
  type: 'message' | 'plan'
  content?: string
  plan?: BuildChatPlan
}

export function useSendBuildChatMessage(): UseMutationResult<BuildChatTurnResponse, Error, string> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (content: string) =>
      apiFetch<BuildChatTurnResponse>('/dashboard/api/ai/build-chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['build-chat-session'] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export interface BuildChatConfirmResponse {
  app: AppDef
}

// useConfirmBuildChatPlan never sends a plan in the request body — the
// server always executes the plan it already persisted on the session
// (AIBC-24) — sessionId in the URL is the only input.
export function useConfirmBuildChatPlan(): UseMutationResult<BuildChatConfirmResponse, Error, string> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (sessionId: string) =>
      apiFetch<BuildChatConfirmResponse>(`/dashboard/api/ai/build-chat/${sessionId}/confirm`, {
        method: 'POST',
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['apps'] })
      qc.invalidateQueries({ queryKey: ['build-chat-session'] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export function useRestartBuildChatSession(): UseMutationResult<BuildChatSessionResponse, Error, void> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () =>
      apiFetch<BuildChatSessionResponse>('/dashboard/api/ai/build-chat/restart', {
        method: 'POST',
      }),
    onSuccess: (data) => {
      qc.setQueryData(['build-chat-session'], data)
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

// ---- Edit with AI chat (ai-edit-chat T11) ----
// Mirrors the build-chat types/hooks above, scoped to one existing app
// instead of a global session: one EditOperation proposed/confirmed at a
// time (never a batched plan), reusing the exact *ForUser response shapes
// (TableDef/AppDef) the manual dashboard form already returns.

export interface EditChatPlanTable {
  name: string
  columns: BuildChatPlanColumn[]
}

export interface EditChatColumnOp {
  table: string
  column: BuildChatPlanColumn
}

export interface EditChatIndexOp {
  table: string
  name: string
  columns: string[]
  unique: boolean
}

export interface EditChatReferenceOp {
  table: string
  column: BuildChatPlanColumn
  ref_table: string
  ref_column: string
  on_delete?: string
}

export interface EditChatRLSOp {
  table: string
  mode: string
}

export interface EditChatAuthOp {
  email_enabled: boolean
}

export interface EditChatForeignKeyOp {
  table: string
  column: string
  ref_table: string
  ref_column: string
  on_delete?: string
}

export interface EditChatRemoveForeignKeyOp {
  table: string
  column: string
}

// EditOperation mirrors internal/dashboard/ai/client.go's Go type of the
// same name — exactly one of the optional fields is populated, matching
// `kind`.
export interface EditOperation {
  kind:
    | 'add_table'
    | 'add_column'
    | 'add_index'
    | 'add_reference'
    | 'add_foreign_key'
    | 'remove_foreign_key'
    | 'set_rls_mode'
    | 'toggle_auth'
  add_table?: EditChatPlanTable
  add_column?: EditChatColumnOp
  add_index?: EditChatIndexOp
  add_reference?: EditChatReferenceOp
  add_foreign_key?: EditChatForeignKeyOp
  remove_foreign_key?: EditChatRemoveForeignKeyOp
  set_rls_mode?: EditChatRLSOp
  toggle_auth?: EditChatAuthOp
}

export interface EditChatMessage {
  id: string
  session_id: string
  role: 'user' | 'assistant'
  content: string
  plan?: EditOperation
  created_at: string
}

export interface EditChatSession {
  id: string
  owner_user_id: string
  status: 'in_progress' | 'completed' | 'abandoned'
  mode: string
  target_app_id?: string
  created_at: string
  updated_at: string
}

export interface EditChatSessionResponse {
  session: EditChatSession
  messages: EditChatMessage[]
}

// useEditChatSession resumes appId's in_progress edit session for the
// caller (or creates one) — `enabled` gates the fetch so the drawer only
// requests this once mounted with `open: true`.
export function useEditChatSession(appId: string, enabled: boolean): UseQueryResult<EditChatSessionResponse> {
  return useQuery({
    queryKey: ['edit-chat-session', appId],
    queryFn: () => apiFetch<EditChatSessionResponse>(`/dashboard/api/apps/${appId}/ai/edit-chat`),
    enabled: enabled && Boolean(appId),
    refetchOnWindowFocus: false,
  })
}

export interface EditChatTurnResponse {
  type: 'message' | 'edit_op'
  content?: string
  edit_op?: EditOperation
}

export function useSendEditChatMessage(appId: string): UseMutationResult<EditChatTurnResponse, Error, string> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (content: string) =>
      apiFetch<EditChatTurnResponse>(`/dashboard/api/apps/${appId}/ai/edit-chat`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['edit-chat-session', appId] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export interface EditChatConfirmResponse {
  applied: boolean
  kind?: string
  table?: TableDef
  app?: AppDef
}

// useConfirmEditChatOperation never sends the operation in the request
// body — the server always re-derives and applies the session's last
// persisted EditOperation (AIEC-16's double-confirm guard) — sessionId in
// the URL is the only input.
export function useConfirmEditChatOperation(appId: string): UseMutationResult<EditChatConfirmResponse, Error, string> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (sessionId: string) =>
      apiFetch<EditChatConfirmResponse>(`/dashboard/api/apps/${appId}/ai/edit-chat/${sessionId}/confirm`, {
        method: 'POST',
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['apps'] })
      qc.invalidateQueries({ queryKey: ['apps', appId] })
      qc.invalidateQueries({ queryKey: ['edit-chat-session', appId] })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export function useRestartEditChatSession(appId: string): UseMutationResult<EditChatSessionResponse, Error, void> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () =>
      apiFetch<EditChatSessionResponse>(`/dashboard/api/apps/${appId}/ai/edit-chat/restart`, {
        method: 'POST',
      }),
    onSuccess: (data) => {
      qc.setQueryData(['edit-chat-session', appId], data)
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

// ---- AI Frontend Builder (ai-frontend-builder T33) ----
// Types mirror internal/dashboard/builder_*.go, builder_settings_store.go and
// builder_templates.go exactly (JSON tags, nullability). Routes live under
// /dashboard/api/builder/*, plus PATCH /dashboard/api/github/templates/{id}/builder.
// No user-facing strings here: callers localize from the codes below.

/** A registered GitHub template (github_templates_store.go GitHubTemplate). */
export interface GitHubTemplate {
  id: string
  name: string
  description: string
  github_owner: string
  github_repo: string
  framework: string
  active: boolean
  created_by: string
  created_at: string
  render_service_type: string
  build_command: string
  publish_path: string
  start_command: string
  /** Marks the template as a base template of the AI Frontend Builder. Independent of `active`. */
  builder_enabled: boolean
}

/** A published frontend app (frontend_apps_store.go FrontendApp). */
export interface FrontendApp {
  id: string
  name: string
  slug: string
  template_id: string
  template_name: string
  github_repo_url: string
  status: string
  error_message?: string
  created_by: string
  created_at: string
  archived_at?: string
  backend_app_id?: string
  deploy_service_id?: string
  deploy_url?: string
  deploy_status?: string
  deploy_error_message?: string
  custom_domain?: string
  owner_id: string
  owner_email?: string
  owner_name?: string
}

// -- Status --

/** Why GET /builder/status says available=false ('' when available). */
export type BuilderUnavailableReason =
  | ''
  | 'not_configured' // BUILDER_URL unset: no builder deployed
  | 'disabled' // superadmin switched it off (settings.enabled)
  | 'provider_not_configured' // the settings' LLM provider is unusable
  | 'builder_unreachable' // builder health probe failed

export interface BuilderStatus {
  available: boolean
  provider_configured: boolean
  reason: BuilderUnavailableReason
}

// -- Sessions / turns --

export type BuilderSessionStatus =
  | 'idle'
  | 'running'
  | 'queued'
  | 'awaiting_confirmation'
  | 'failed'
  | 'archived'

export type BuilderTurnStatus =
  | 'pending'
  | 'queued'
  | 'running'
  | 'awaiting_confirmation'
  | 'completed'
  | 'failed'
  | 'limit_reached'
  | 'budget_exceeded'

export type BuilderTurnKind = 'chat' | 'revert'

/** Generic turn error codes (agent/limits.go, agent/revert.go). Open set: treat unknown as generic. */
export type BuilderTurnErrorCode =
  | 'provider_unavailable'
  | 'context_too_long'
  | 'workspace_lost'
  | 'tool_failed'
  | 'commit_failed'
  | 'internal_error'
  | 'unsupported_turn_kind'
  | 'revert_failed'

/** `limit` of a limit_reached event: which cap ended the turn. */
export type BuilderLimitKind = 'max_steps' | 'max_fix_iterations' | 'max_turn_duration'

/**
 * The model's propose_backend_change arguments: {"operation": {"kind": ..., "<kind>": {...}}}.
 * Schema names only, never row data. The shape under each kind is not fixed here.
 */
export interface BuilderProposalOperation {
  operation?: { kind?: string; [key: string]: unknown }
  [key: string]: unknown
}

export interface BuilderProposal {
  tool_call_id: string
  name: string
  operation: BuilderProposalOperation
}

export interface BuilderTurn {
  id: string
  seq: number
  kind: BuilderTurnKind
  status: BuilderTurnStatus
  commit_sha: string
  error_code: BuilderTurnErrorCode | string | null
  /** The user's message that opened the turn; absent for revert turns. */
  user_message?: string
  /** Present only while status === 'awaiting_confirmation'. */
  pending_proposal?: BuilderProposal
  created_at: string
  finished_at: string | null
}

export interface BuilderSession {
  id: string
  backend_app_id: string
  title: string
  template_kind: 'default' | 'github'
  template_id: string | null
  template_version: string
  prompt_version_id: string | null
  locale: string
  status: BuilderSessionStatus
  head_sha: string
  head_turn_seq: number
  frontend_app_id: string | null
  published_sha: string
  tokens_in: number
  tokens_out: number
  created_at: string
  updated_at: string
}

export interface BuilderSessionDetail extends BuilderSession {
  turns: BuilderTurn[]
}

/** Body of POST /builder/sessions. Empty/absent template_id selects the built-in default template. */
export interface CreateBuilderSessionInput {
  backend_app_id: string
  template_id?: string
  /** Defaults to the backend app's name; max 120 characters. */
  title?: string
  /** BCP-47-ish tag ('pt-BR'); defaults to Accept-Language, then 'en'. The agent replies in it. */
  locale?: string
}

// -- Events --

export interface BuilderEventPayloads {
  turn_started: { seq?: number; resumed?: boolean; kind?: string }
  queued: Record<string, never>
  assistant_message: { content?: string }
  tool_call: { name?: string; summary?: string }
  file_changed: { path?: string; op?: string }
  build_started: { step?: string }
  build_succeeded: { step?: string }
  build_failed: { step?: string; tail?: string }
  proposal: { tool_call_id?: string; name?: string; operation?: BuilderProposalOperation }
  limit_reached: { limit?: BuilderLimitKind | string }
  budget_exceeded: Record<string, never>
  error: { code?: BuilderTurnErrorCode | string }
  revert: {
    to_turn_seq?: number
    sha?: string
    changed?: boolean
    build?: 'succeeded' | 'failed' | 'unavailable' | string
  }
  turn_finished: { status?: BuilderTurnStatus | string; sha?: string }
  // Written by the proposal and publish handlers; only short scalar fields are served.
  proposal_confirmed: { tool_call_id?: string; applied?: boolean; kind?: string }
  proposal_rejected: { tool_call_id?: string }
  publish_started: { republish?: boolean }
  publish_succeeded: { republish?: boolean; sha?: string }
  publish_failed: { code?: BuilderPublishConflictReason | 'github_failed' | string }
}

export type BuilderEventType = keyof BuilderEventPayloads

/**
 * One entry of the event log (BuilderEventView). Payload keys are whitelisted server side and
 * every key is optional. The set of types can grow: ignore a `type` you do not know.
 */
export type BuilderEvent = {
  [K in BuilderEventType]: {
    id: number
    turn_id: string | null
    type: K
    payload: BuilderEventPayloads[K]
    created_at: string
  }
}[BuilderEventType]

/** Wire shape of GET /builder/sessions/{id}/events. */
export interface BuilderEventsPage {
  events: BuilderEvent[]
  /** Equals the request's `after` when nothing is new. */
  next_cursor: number
}

// -- Messages / revert / proposals --

/** Body of POST /builder/sessions/{id}/revert. turn_seq 0 is the initial template; always send it. */
export interface RevertBuilderSessionInput {
  turn_seq: number
}

/** Body of the confirm and reject routes (builderProposalResult). */
export interface BuilderProposalResult {
  applied: boolean
  rejected?: boolean
  kind?: string
  /** A repeated request that did nothing. */
  already_resolved?: boolean
  turn?: BuilderTurn
}

// -- Preview / publish --

/** Body of POST /builder/sessions/{id}/preview-token. Keep `token` out of logs and visible URLs. */
export interface BuilderPreviewToken {
  token: string
  expires_at: string
  /** The iframe src: same-origin path with ?pt=<token>. Mint a new one before expires_at (10 minutes). */
  url: string
}

/** Body of POST /builder/sessions/{id}/publish. Optional; lowercase DNS label. */
export interface PublishBuilderSessionInput {
  subdomain?: string
}

export interface BuilderPublishResult {
  frontend_app: FrontendApp
  published_sha: string
  /** True when an existing frontend app was updated. */
  republish: boolean
}

/** `reason` of a 409 from the publish route (ApiError.reason). */
export type BuilderPublishConflictReason =
  | 'github_not_configured'
  | 'deploy_not_configured'
  | 'template_not_deployable'
  | 'template_unavailable'
  | 'nothing_to_publish'
  | 'turn_in_progress'
  | 'publish_in_progress'
  | 'remote_moved'

// -- Templates --

export interface BuilderTemplate {
  /** 'default' has an empty id; pass a 'github' template's id as template_id when creating a session. */
  kind: 'default' | 'github'
  id: string
  name: string
  description: string
  framework: string
  render_service_type: string
  build_command: string
  publish_path: string
}

// -- Settings (superadmin) --

export type BuilderProviderKind = 'openai' | 'openai_compatible'

export interface BuilderSettings {
  enabled: boolean
  provider: BuilderProviderKind | string
  context_window_tokens: number
  /** Tokens (in + out). 0 = unset; sessions stay blocked until both caps are positive. */
  session_token_cap: number
  user_monthly_token_cap: number
  max_steps_per_turn: number
  max_fix_iterations: number
  max_turn_seconds: number
  updated_by: string | null
  updated_at: string | null
  /** false until the first save (defaults shown). */
  configured: boolean
}

/**
 * Body of PUT /builder/settings (BuilderSettingsInput): merge-on-absent-key. A key that is
 * absent (or undefined, which JSON drops) keeps the stored value; at least one key is required.
 * To "clear" a cap send 0 explicitly. enabled/provider/limits have no empty value: send the
 * new value. Never send NaN/null (JSON turns NaN into null, which the server reads as absent).
 */
export interface BuilderSettingsUpdate {
  enabled?: boolean
  provider?: BuilderProviderKind
  context_window_tokens?: number
  session_token_cap?: number
  user_monthly_token_cap?: number
  max_steps_per_turn?: number
  max_fix_iterations?: number
  max_turn_seconds?: number
}

/** Server-side bounds of the numeric settings (builder_settings.go), for form validation. */
export const BUILDER_SETTINGS_LIMITS = {
  context_window_tokens: { min: 4096, max: 2_000_000 },
  session_token_cap: { min: 0, max: 1_000_000_000_000 },
  user_monthly_token_cap: { min: 0, max: 1_000_000_000_000 },
  max_steps_per_turn: { min: 1, max: 200 },
  max_fix_iterations: { min: 1, max: 20 },
  max_turn_seconds: { min: 10, max: 3600 },
} as const

// -- Persona + rules (superadmin) --

export interface BuilderPrompt {
  /** '' until the first save. */
  id: string
  persona: string
  rules: string
  created_by: string
  created_by_email: string
  created_at: string | null
  /** Size of the company layer (persona + rules) and its save-time cap. */
  chars: number
  max_chars: number
  /** Set by PUT only: false when the save matched the active version (no new version). */
  changed?: boolean
}

/**
 * Body of PUT /builder/prompt (BuilderPromptInput): merge-on-absent-key against the active
 * version. Absent = keep; "" = clear. At least one key is required, and a form that lets the
 * user empty a field MUST send that key as "" (never omit it, omission silently no-ops).
 */
export interface BuilderPromptUpdate {
  persona?: string
  rules?: string
}

export interface BuilderPromptVersion {
  id: string
  persona: string
  rules: string
  created_by: string
  created_by_email: string
  created_at: string
}

export interface BuilderPromptVersionsPage {
  versions: BuilderPromptVersion[]
  total: number
}

// -- Helpers --

/** Poll period of the session detail and events while a turn is in flight. */
export const BUILDER_POLL_INTERVAL_MS = 1500

/**
 * True while the builder is working on the session: the session is running/queued, or a turn is
 * still pending/queued/running. The turn check matters: right after a message the turn is
 * 'pending' while the session status is still 'idle'. 'awaiting_confirmation' is NOT active
 * (nothing happens until the user confirms or rejects).
 */
export function isBuilderSessionActive(session: BuilderSessionDetail | undefined): boolean {
  if (!session) return false
  if (session.status === 'running' || session.status === 'queued') return true
  return session.turns.some(
    (t) => t.status === 'pending' || t.status === 'queued' || t.status === 'running',
  )
}

export type BuilderApiErrorKind =
  | 'not_configured' // 503 "builder not configured": no builder deployed
  | 'unavailable' // 503 "builder unavailable": disabled, provider missing or builder down
  | 'turn_active' // 409 on message/revert: a turn is already in progress
  | 'publish_conflict' // 409 with a `reason` (see BuilderPublishConflictReason)
  | 'conflict' // any other 409
  | 'not_found'
  | 'forbidden'
  | 'other'

/**
 * Classifies a builder API failure so the UI can pick a state without parsing text. Returns
 * 'other' for anything that is not an ApiError. Budget and limit outcomes are not HTTP errors:
 * read them from BuilderTurn.status ('budget_exceeded' | 'limit_reached') and the events.
 */
export function classifyBuilderError(error: unknown): {
  kind: BuilderApiErrorKind
  reason?: BuilderPublishConflictReason | string
} {
  if (!(error instanceof ApiError)) return { kind: 'other' }
  switch (error.status) {
    case 503:
      return { kind: error.message === 'builder not configured' ? 'not_configured' : 'unavailable' }
    case 409:
      if (error.reason) return { kind: 'publish_conflict', reason: error.reason }
      if (error.message === 'a turn is already in progress') return { kind: 'turn_active' }
      return { kind: 'conflict' }
    case 404:
      return { kind: 'not_found' }
    case 403:
      return { kind: 'forbidden' }
    default:
      return { kind: 'other' }
  }
}

const BUILDER_BASE = '/dashboard/api/builder'

export const builderKeys = {
  all: ['builder'] as const,
  status: () => ['builder', 'status'] as const,
  sessions: () => ['builder', 'sessions'] as const,
  session: (id: string) => ['builder', 'session', id] as const,
  events: (id: string) => ['builder', 'events', id] as const,
  templates: () => ['builder', 'templates'] as const,
  settings: () => ['builder', 'settings'] as const,
  prompt: () => ['builder', 'prompt'] as const,
  promptVersions: (limit?: number, offset?: number) =>
    ['builder', 'prompt-versions', limit ?? null, offset ?? null] as const,
}

function builderSessionPath(sessionId: string): string {
  return `${BUILDER_BASE}/sessions/${encodeURIComponent(sessionId)}`
}

function jsonRequest(method: string, body?: unknown): RequestInit {
  return body === undefined
    ? { method }
    : { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }
}

/** URL of the ZIP download of a session's project. Use it as an <a href> (same-origin, cookie auth). */
export function builderDownloadUrl(sessionId: string): string {
  return `${builderSessionPath(sessionId)}/download.zip`
}

// -- Queries --

/** Any authenticated user. Answers 200 even when no builder is deployed (reason 'not_configured'). */
export function useBuilderStatus(): UseQueryResult<BuilderStatus> {
  return useQuery({
    queryKey: builderKeys.status(),
    queryFn: () => apiFetch<BuilderStatus>(`${BUILDER_BASE}/status`),
    staleTime: 15000,
  })
}

/** The caller's own non-archived sessions, most recently updated first. */
export function useBuilderSessions(): UseQueryResult<BuilderSession[]> {
  return useQuery({
    queryKey: builderKeys.sessions(),
    queryFn: () => apiFetch<BuilderSession[]>(`${BUILDER_BASE}/sessions`),
  })
}

/** Session + turns (owner only). Polls while a turn is in flight (see isBuilderSessionActive). */
export function useBuilderSession(sessionId: string): UseQueryResult<BuilderSessionDetail> {
  return useQuery({
    queryKey: builderKeys.session(sessionId),
    queryFn: () => apiFetch<BuilderSessionDetail>(builderSessionPath(sessionId)),
    enabled: Boolean(sessionId),
    retry: (count, error) => !(error instanceof ApiError && error.status === 404) && count < 2,
    refetchInterval: (query) =>
      query.state.status !== 'error' && isBuilderSessionActive(query.state.data)
        ? BUILDER_POLL_INTERVAL_MS
        : false,
  })
}

/** What useBuilderEvents returns: every event fetched so far (oldest first) and the cursor. */
export interface BuilderEventsState {
  events: BuilderEvent[]
  next_cursor: number
}

const BUILDER_EVENTS_PAGE_LIMIT = 500 // store.MaxEventsLimit
const BUILDER_EVENTS_MAX_PAGES = 20 // per fetch; the next poll continues from the cursor

/**
 * The session's event log, accumulated: each fetch asks only for events after the cursor kept in
 * the cache and appends them, so callers never manage the cursor. Polls every
 * BUILDER_POLL_INTERVAL_MS only while the session is active (see isBuilderSessionActive) and
 * does one last fetch when it goes inactive, so the closing events are never missed. It reads
 * the session through useBuilderSession (same cache entry, no extra request).
 */
export function useBuilderEvents(sessionId: string): UseQueryResult<BuilderEventsState> {
  const qc = useQueryClient()
  const session = useBuilderSession(sessionId)
  const active = isBuilderSessionActive(session.data)

  const wasActive = useRef(false)
  useEffect(() => {
    if (wasActive.current && !active && sessionId) {
      void qc.invalidateQueries({ queryKey: builderKeys.events(sessionId) })
    }
    wasActive.current = active
  }, [active, sessionId, qc])

  return useQuery({
    queryKey: builderKeys.events(sessionId),
    enabled: Boolean(sessionId),
    retry: (count, error) => !(error instanceof ApiError && error.status === 404) && count < 2,
    refetchInterval: (query) =>
      active && query.state.status !== 'error' ? BUILDER_POLL_INTERVAL_MS : false,
    queryFn: async (): Promise<BuilderEventsState> => {
      const prev = qc.getQueryData<BuilderEventsState>(builderKeys.events(sessionId))
      let cursor = prev?.next_cursor ?? 0
      const events = prev ? [...prev.events] : []
      let added = 0
      for (let i = 0; i < BUILDER_EVENTS_MAX_PAGES; i++) {
        const page = await apiFetch<BuilderEventsPage>(
          `${builderSessionPath(sessionId)}/events?after=${cursor}&limit=${BUILDER_EVENTS_PAGE_LIMIT}`,
        )
        if (page.events.length === 0) break
        events.push(...page.events)
        added += page.events.length
        cursor = page.next_cursor
        if (page.events.length < BUILDER_EVENTS_PAGE_LIMIT) break
      }
      // Nothing new: keep the same object so subscribers do not re-render every poll.
      return prev && added === 0 ? prev : { events, next_cursor: cursor }
    },
  })
}

/** Default template first, then the builder-enabled company templates. Any authenticated user. */
export function useBuilderTemplates(): UseQueryResult<BuilderTemplate[]> {
  return useQuery({
    queryKey: builderKeys.templates(),
    queryFn: () => apiFetch<BuilderTemplate[]>(`${BUILDER_BASE}/templates`),
    staleTime: 30000,
  })
}

// Superadmin-only routes (403 for anyone else): pass { enabled: false } to skip the request.
export interface BuilderAdminQueryOptions {
  enabled?: boolean
}

export function useBuilderSettings(
  options: BuilderAdminQueryOptions = {},
): UseQueryResult<BuilderSettings> {
  return useQuery({
    queryKey: builderKeys.settings(),
    queryFn: () => apiFetch<BuilderSettings>(`${BUILDER_BASE}/settings`),
    enabled: options.enabled ?? true,
  })
}

/** The active persona + rules (the newest version; empty fields and id '' before the first save). */
export function useBuilderPrompt(
  options: BuilderAdminQueryOptions = {},
): UseQueryResult<BuilderPrompt> {
  return useQuery({
    queryKey: builderKeys.prompt(),
    queryFn: () => apiFetch<BuilderPrompt>(`${BUILDER_BASE}/prompt`),
    enabled: options.enabled ?? true,
  })
}

/** Full prompt history, newest first. limit defaults to 20 (max 100) server side. */
export function useBuilderPromptVersions(
  params: { limit?: number; offset?: number } = {},
  options: BuilderAdminQueryOptions = {},
): UseQueryResult<BuilderPromptVersionsPage> {
  const search = new URLSearchParams()
  if (params.limit !== undefined) search.set('limit', String(params.limit))
  if (params.offset !== undefined) search.set('offset', String(params.offset))
  const qs = search.toString()
  return useQuery({
    queryKey: builderKeys.promptVersions(params.limit, params.offset),
    queryFn: () =>
      apiFetch<BuilderPromptVersionsPage>(`${BUILDER_BASE}/prompt/versions${qs ? `?${qs}` : ''}`),
    enabled: options.enabled ?? true,
  })
}

// -- Mutations --

export function useUpdateBuilderSettings(): UseMutationResult<
  BuilderSettings,
  Error,
  BuilderSettingsUpdate
> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: BuilderSettingsUpdate) =>
      apiFetch<BuilderSettings>(`${BUILDER_BASE}/settings`, jsonRequest('PUT', input)),
    onSuccess: (data) => {
      qc.setQueryData(builderKeys.settings(), data)
      // enabled/provider decide the availability the status endpoint reports.
      qc.invalidateQueries({ queryKey: builderKeys.status() })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export function useUpdateBuilderPrompt(): UseMutationResult<
  BuilderPrompt,
  Error,
  BuilderPromptUpdate
> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: BuilderPromptUpdate) =>
      apiFetch<BuilderPrompt>(`${BUILDER_BASE}/prompt`, jsonRequest('PUT', input)),
    onSuccess: (data) => {
      qc.setQueryData(builderKeys.prompt(), data)
      if (data.changed) {
        qc.invalidateQueries({ queryKey: ['builder', 'prompt-versions'] })
      }
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

/** PATCH /github/templates/{id}/builder (superadmin): returns the updated template. */
export function useSetBuilderTemplateEnabled(): UseMutationResult<
  GitHubTemplate,
  Error,
  { id: string; builder_enabled: boolean }
> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, builder_enabled }) =>
      apiFetch<GitHubTemplate>(
        `/dashboard/api/github/templates/${encodeURIComponent(id)}/builder`,
        jsonRequest('PATCH', { builder_enabled }),
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: builderKeys.templates() })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

export function useCreateBuilderSession(): UseMutationResult<
  BuilderSession,
  Error,
  CreateBuilderSessionInput
> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: CreateBuilderSessionInput) =>
      apiFetch<BuilderSession>(`${BUILDER_BASE}/sessions`, jsonRequest('POST', input)),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: builderKeys.sessions() })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

/** Archives the session and destroys its workspace (204). Published frontend apps are untouched. */
export function useDeleteBuilderSession(): UseMutationResult<void, Error, string> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (sessionId: string) =>
      apiFetch<void>(builderSessionPath(sessionId), { method: 'DELETE' }),
    onSuccess: (_data, sessionId) => {
      qc.removeQueries({ queryKey: builderKeys.session(sessionId) })
      qc.removeQueries({ queryKey: builderKeys.events(sessionId) })
      qc.invalidateQueries({ queryKey: builderKeys.sessions() })
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

// After any mutation that creates or resolves a turn the session detail (status, turns) and
// the event log are stale; refetching them also (re)starts the polling.
function invalidateBuilderSession(qc: ReturnType<typeof useQueryClient>, sessionId: string): void {
  qc.invalidateQueries({ queryKey: builderKeys.session(sessionId) })
  qc.invalidateQueries({ queryKey: builderKeys.events(sessionId) })
}

/** Enqueues a chat turn (202 with the new pending turn). 409 when a turn is already in progress. */
export function useSendBuilderMessage(
  sessionId: string,
): UseMutationResult<BuilderTurn, Error, string> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (content: string) =>
      apiFetch<BuilderTurn>(
        `${builderSessionPath(sessionId)}/messages`,
        jsonRequest('POST', { content }),
      ),
    onSuccess: () => {
      invalidateBuilderSession(qc, sessionId)
      qc.invalidateQueries({ queryKey: builderKeys.sessions() })
    },
    onError: (error) => {
      // BuilderPage shows a localized inline notice for a concurrent turn (409).
      if (classifyBuilderError(error).kind === 'turn_active') return
      toast.error(error.message)
    },
  })
}

/** Restores the state a previous turn left (as a new revert turn). Variable: the target turn_seq (0 = template). */
export function useRevertBuilderSession(
  sessionId: string,
): UseMutationResult<BuilderTurn, Error, number> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (turnSeq: number) =>
      apiFetch<BuilderTurn>(
        `${builderSessionPath(sessionId)}/revert`,
        jsonRequest('POST', { turn_seq: turnSeq } satisfies RevertBuilderSessionInput),
      ),
    onSuccess: () => {
      invalidateBuilderSession(qc, sessionId)
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

function builderTurnPath(
  sessionId: string,
  turn: string | number,
  action: 'confirm' | 'reject',
): string {
  return `${builderSessionPath(sessionId)}/turns/${encodeURIComponent(String(turn))}/${action}`
}

/**
 * Confirms the paused proposal of a turn (variable: turn id or seq). Applies a schema change to
 * the BACKEND app, so it needs write access there (403/404 otherwise, the turn stays paused).
 * A change the app refuses answers 400 with a safe message AND resumes the turn, so the session
 * is refreshed on error as well (onSettled).
 */
export function useConfirmBuilderTurn(
  sessionId: string,
): UseMutationResult<BuilderProposalResult, Error, string | number> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (turn: string | number) =>
      apiFetch<BuilderProposalResult>(builderTurnPath(sessionId, turn, 'confirm'), {
        method: 'POST',
      }),
    onSuccess: () => {
      // The backend app's schema changed.
      qc.invalidateQueries({ queryKey: ['apps'] })
    },
    onSettled: () => {
      invalidateBuilderSession(qc, sessionId)
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

/** Rejects the paused proposal of a turn (variable: turn id or seq). Applies nothing. */
export function useRejectBuilderTurn(
  sessionId: string,
): UseMutationResult<BuilderProposalResult, Error, string | number> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (turn: string | number) =>
      apiFetch<BuilderProposalResult>(builderTurnPath(sessionId, turn, 'reject'), {
        method: 'POST',
      }),
    onSettled: () => {
      invalidateBuilderSession(qc, sessionId)
    },
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

/**
 * Mints a preview token (POST; the answer is an iframe src that expires, so this is a mutation,
 * not a cached query). Re-mint before `expires_at`. The iframe must be sandboxed WITHOUT
 * allow-same-origin (design section 10).
 */
export function useBuilderPreviewToken(
  sessionId: string,
): UseMutationResult<BuilderPreviewToken, Error, void> {
  return useMutation({
    mutationFn: () =>
      apiFetch<BuilderPreviewToken>(`${builderSessionPath(sessionId)}/preview-token`, {
        method: 'POST',
      }),
    onError: (error) => {
      toast.error(error.message)
    },
  })
}

/**
 * Publishes (first time) or republishes the session as a frontend app. Precondition failures
 * are 409 with an ApiError.reason (BuilderPublishConflictReason); use classifyBuilderError.
 * The variable is optional: call .mutate() or .mutate({ subdomain }).
 */
export function usePublishBuilderSession(
  sessionId: string,
): UseMutationResult<BuilderPublishResult, Error, PublishBuilderSessionInput | void> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input?: PublishBuilderSessionInput | void) =>
      apiFetch<BuilderPublishResult>(
        `${builderSessionPath(sessionId)}/publish`,
        jsonRequest('POST', input ?? {}),
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['frontend-apps'] })
      qc.invalidateQueries({ queryKey: builderKeys.sessions() })
    },
    // A failed publish can still have recorded publish_* events or changed the session.
    onSettled: () => {
      invalidateBuilderSession(qc, sessionId)
    },
    onError: (error) => {
      // BuilderPage shows a localized inline message for the publish preconditions (409).
      if (classifyBuilderError(error).kind === 'publish_conflict') return
      toast.error(error.message)
    },
  })
}
