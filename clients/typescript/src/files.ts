import { ClientConfig, FileResponse, SignedURLResponse, ListResponse } from './types'

export class FilesClient {
  constructor(private config: ClientConfig) {}

  private url(path: string): string {
    return `${this.config.baseURL}/${this.config.app}/files/${path}`
  }

  private headers(): Record<string, string> {
    return { Authorization: `Bearer ${this.config.jwt}` }
  }

  async upload(file: File | Blob, filename?: string): Promise<FileResponse> {
    const form = new FormData()
    form.append('file', file, filename)
    const res = await fetch(this.url(''), {
      method: 'POST',
      headers: { Authorization: `Bearer ${this.config.jwt}` },
      body: form,
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: res.statusText }))
      throw new Error(err.error || `HTTP ${res.status}`)
    }
    return res.json()
  }

  async list(limit = 50, offset = 0): Promise<FileResponse[]> {
    const res = await fetch(this.url(`?limit=${limit}&offset=${offset}`), {
      headers: this.headers(),
    })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return res.json()
  }

  async get(id: string): Promise<FileResponse> {
    const res = await fetch(this.url(id), { headers: this.headers() })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return res.json()
  }

  async delete(id: string): Promise<void> {
    const res = await fetch(this.url(id), {
      method: 'DELETE',
      headers: this.headers(),
    })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
  }

  async signedURL(id: string, ttl = 3600): Promise<string> {
    const res = await fetch(this.url(`${id}/url?ttl=${ttl}`), {
      headers: this.headers(),
    })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    const data: SignedURLResponse = await res.json()
    return data.url
  }
}
