export interface ClientConfig {
  baseURL: string
  app: string
  jwt: string
}

export type ColumnType = 'text' | 'integer' | 'bigint' | 'decimal' | 'boolean' | 'uuid' | 'timestamptz' | 'jsonb'

export type FilterOperator = 'eq' | 'ne' | 'gt' | 'gte' | 'lt' | 'lte' | 'like' | 'ilike' | 'in'

export type FilterValue = string | number | boolean | string[]

export interface FindManyParams {
  limit?: number
  offset?: number
  order?: string
  filters?: Record<string, string>
}

export interface ListResponse<T = Record<string, unknown>> {
  data: T[]
  count: number
  limit: number
  offset: number
}

export interface AuthLoginParams {
  email: string
  password: string
}

export interface AuthRegisterParams {
  email: string
  password: string
  name?: string
}

export interface AuthResponse {
  token: string
  refresh_token: string
  user: {
    id: string
    email: string
    name?: string
  }
}

export interface AuthUser {
  id: string
  email: string
  name?: string
  phone?: string
  avatar_url?: string
}

export interface FileResponse {
  id: string
  name: string
  size: number
  mime_type: string
  url: string
  created_at: string
}

export interface SignedURLResponse {
  url: string
}
