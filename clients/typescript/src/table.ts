import { ClientConfig, ListResponse, FindManyParams } from './types'

export class TableClient {
  constructor(private config: ClientConfig, private table: string) {}

  private url(path: string): string {
    return `${this.config.baseURL}/${this.config.app}/${path}`
  }

  private headers(): Record<string, string> {
    return {
      Authorization: `Bearer ${this.config.jwt}`,
      'Content-Type': 'application/json',
    }
  }

  private async request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const res = await fetch(this.url(path), {
      method,
      headers: this.headers(),
      body: body ? JSON.stringify(body) : undefined,
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: res.statusText }))
      throw new Error(err.error || `HTTP ${res.status}`)
    }
    if (res.status === 204) return undefined as T
    return res.json()
  }

  async findMany(params?: FindManyParams): Promise<ListResponse> {
    const qs = new URLSearchParams()
    if (params?.limit) qs.set('limit', String(params.limit))
    if (params?.offset) qs.set('offset', String(params.offset))
    if (params?.order) qs.set('order', params.order)
    if (params?.filters) {
      for (const [col, val] of Object.entries(params.filters)) {
        qs.set(col, val)
      }
    }
    const q = qs.toString()
    return this.request('GET', `${this.table}/${q ? `?${q}` : ''}`)
  }

  async findById(id: string): Promise<Record<string, unknown>> {
    return this.request('GET', `${this.table}/${id}`)
  }

  async create(data: Record<string, unknown>): Promise<Record<string, unknown>> {
    return this.request('POST', this.table, data)
  }

  async update(id: string, data: Record<string, unknown>): Promise<Record<string, unknown>> {
    return this.request('PATCH', `${this.table}/${id}`, data)
  }

  async replace(id: string, data: Record<string, unknown>): Promise<Record<string, unknown>> {
    return this.request('PUT', `${this.table}/${id}`, data)
  }

  async delete(id: string): Promise<void> {
    return this.request('DELETE', `${this.table}/${id}`)
  }
}
