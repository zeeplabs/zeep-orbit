import { useState, useEffect, useCallback } from 'react'
import type { OrbitConfig } from '../types'

interface FilesPageProps {
  config: OrbitConfig
  onLogout: () => void
  onNavigate: (page: 'todos' | 'files') => void
}

interface FileItem {
  id: string
  name: string
  size: number
  mime_type: string
  url: string
  created_at: string
}

export function FilesPage({ config, onLogout, onNavigate }: FilesPageProps) {
  const [files, setFiles] = useState<FileItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [uploading, setUploading] = useState(false)
  const [selectedFile, setSelectedFile] = useState<File | null>(null)
  const [signedUrl, setSignedUrl] = useState('')
  const [signedUrlTtl, setSignedUrlTtl] = useState('3600')

  const baseUrl = `${config.baseURL}/${config.app}/files`

  const headers = (): Record<string, string> => ({
    Authorization: `Bearer ${config.jwt ?? ''}`,
  })

  const fetchFiles = useCallback(async () => {
    try {
      const res = await fetch(baseUrl + '/', { headers: headers() })
      if (!res.ok) throw new Error(`HTTP ${res.status}: ${res.statusText}`)
      const data = await res.json()
      setFiles(data)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load files')
    } finally {
      setLoading(false)
    }
  }, [baseUrl, config.jwt])

  useEffect(() => {
    fetchFiles()
  }, [fetchFiles])

  const handleUpload = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!selectedFile) return

    setUploading(true)
    setError('')

    try {
      const formData = new FormData()
      formData.append('file', selectedFile)

      const res = await fetch(baseUrl + '/', {
        method: 'POST',
        headers: headers(),
        body: formData,
      })

      if (!res.ok) {
        const text = await res.text()
        throw new Error(`HTTP ${res.status}: ${text}`)
      }

      setSelectedFile(null)
      await fetchFiles()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Upload failed')
    } finally {
      setUploading(false)
    }
  }

  const handleDelete = async (id: string) => {
    try {
      const res = await fetch(`${baseUrl}/${id}`, {
        method: 'DELETE',
        headers: headers(),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      setFiles((prev) => prev.filter((f) => f.id !== id))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Delete failed')
    }
  }

  const handleGetSignedUrl = async (id: string) => {
    try {
      const ttl = parseInt(signedUrlTtl, 10) || 3600
      const res = await fetch(`${baseUrl}/${id}/url?ttl=${ttl}`, {
        headers: headers(),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data = await res.json()
      setSignedUrl(data.url)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to get signed URL')
    }
  }

  const handleDownload = async (id: string) => {
    try {
      const ttl = parseInt(signedUrlTtl, 10) || 3600
      const res = await fetch(`${baseUrl}/${id}/url?ttl=${ttl}`, {
        headers: headers(),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data = await res.json()
      window.open(data.url, '_blank')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Download failed')
    }
  }

  const formatSize = (bytes: number): string => {
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  }

  return (
    <div className="page">
      <div className="card" style={{ maxWidth: 640 }}>
        <div className="header-row">
          <div>
            <h1>Files</h1>
            <p className="subtitle">
              {config.baseURL}/{config.app}
            </p>
          </div>
          <div className="header-actions">
            <button className="btn-ghost" onClick={() => onNavigate('todos')}>
              Todos
            </button>
            <button className="btn-ghost" onClick={onLogout}>
              Logout
            </button>
          </div>
        </div>

        {/* Upload */}
        <form className="upload-form" onSubmit={handleUpload}>
          <div className="upload-area">
            <input
              type="file"
              id="file-input"
              onChange={(e) => setSelectedFile(e.target.files?.[0] ?? null)}
              className="file-input"
            />
            <label htmlFor="file-input" className="file-label">
              {selectedFile ? selectedFile.name : 'Choose a file…'}
            </label>
          </div>
          <button
            type="submit"
            className="btn-primary"
            disabled={!selectedFile || uploading}
          >
            {uploading ? 'Uploading…' : 'Upload'}
          </button>
        </form>

        {/* Signed URL tool */}
        <div className="signed-url-tool">
          <input
            type="number"
            value={signedUrlTtl}
            onChange={(e) => setSignedUrlTtl(e.target.value)}
            min={1}
            max={86400}
            className="ttl-input"
            title="TTL in seconds"
          />
          <span className="ttl-label">s TTL</span>
        </div>

        {signedUrl && (
          <div className="signed-url-box">
            <code className="signed-url-text">{signedUrl}</code>
            <button
              className="btn-sm"
              onClick={() => { navigator.clipboard.writeText(signedUrl); setSignedUrl('') }}
            >
              Copy
            </button>
          </div>
        )}

        {error && <p className="error">{error}</p>}

        {/* File list */}
        {loading ? (
          <p className="loading">Loading files…</p>
        ) : files.length === 0 ? (
          <p className="empty">No files yet. Upload one above!</p>
        ) : (
          <ul className="file-list">
            {files.map((file) => (
              <li key={file.id} className="file-item">
                <div className="file-info">
                  <span className="file-name">{file.name}</span>
                  <span className="file-meta">
                    {formatSize(file.size)} &middot; {file.mime_type}
                  </span>
                </div>
                <div className="file-actions">
                  <button className="btn-sm" onClick={() => handleDownload(file.id)}>
                    Download
                  </button>
                  <button className="btn-sm" onClick={() => handleGetSignedUrl(file.id)}>
                    Signed URL
                  </button>
                  <button className="btn-sm btn-danger" onClick={() => handleDelete(file.id)}>
                    Delete
                  </button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
