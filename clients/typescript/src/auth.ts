import {
  ClientConfig,
  AuthLoginParams,
  AuthRegisterParams,
  AuthResponse,
  AuthUser,
} from './types'

export class AuthClient {
  constructor(private config: ClientConfig) {}

  private url(path: string): string {
    return `${this.config.baseURL}/${this.config.app}/auth/${path}`
  }

  private headers(): Record<string, string> {
    return { 'Content-Type': 'application/json' }
  }

  private authHeaders(): Record<string, string> {
    return {
      Authorization: `Bearer ${this.config.jwt}`,
      'Content-Type': 'application/json',
    }
  }

  private async request<T>(method: string, path: string, body?: unknown, authenticated = false): Promise<T> {
    const res = await fetch(this.url(path), {
      method,
      headers: authenticated ? this.authHeaders() : this.headers(),
      body: body ? JSON.stringify(body) : undefined,
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: res.statusText }))
      throw new Error(err.error || `HTTP ${res.status}`)
    }
    return res.json()
  }

  async register(params: AuthRegisterParams): Promise<AuthResponse> {
    return this.request('POST', 'register', params)
  }

  async login(params: AuthLoginParams): Promise<AuthResponse> {
    return this.request('POST', 'login', params)
  }

  async me(): Promise<AuthUser> {
    return this.request('GET', 'me', undefined, true)
  }

  async updateMe(data: Partial<AuthUser>): Promise<AuthUser> {
    return this.request('PUT', 'me', data, true)
  }

  async refresh(): Promise<{ token: string }> {
    return this.request('POST', 'refresh')
  }

  async logout(): Promise<void> {
    await this.request('POST', 'logout', undefined, true)
  }
}
