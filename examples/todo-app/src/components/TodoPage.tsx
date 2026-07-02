import { useState, useEffect, useCallback } from 'react'
import type { OrbitConfig, Todo } from '../types'
import { createClient } from '../orbit'

interface TodoPageProps {
  config: OrbitConfig
  onLogout: () => void
  onNavigate: (page: 'todos' | 'files') => void
}

export function TodoPage({ config, onLogout, onNavigate }: TodoPageProps) {
  const [todos, setTodos] = useState<Todo[]>([])
  const [newTitle, setNewTitle] = useState('')
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editTitle, setEditTitle] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)

  const client = createClient(config)
  const todosTable = client.table('todos')

  const fetchTodos = useCallback(async () => {
    try {
      const res = await todosTable.findMany({ order: 'created_at.desc' })
      setTodos(res.data as Todo[])
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load todos')
    } finally {
      setLoading(false)
    }
  }, [config.baseURL, config.app, config.jwt])

  useEffect(() => {
    fetchTodos()
  }, [fetchTodos])

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    const title = newTitle.trim()
    if (!title) return

    try {
      const created = await todosTable.create({ title, completed: false })
      setTodos((prev) => [...prev, created as Todo])
      setNewTitle('')
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create todo')
    }
  }

  const handleToggle = async (todo: Todo) => {
    try {
      const updated = await todosTable.update(todo.id, { completed: !todo.completed })
      setTodos((prev) => prev.map((t) => (t.id === todo.id ? (updated as Todo) : t)))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update todo')
    }
  }

  const handleStartEdit = (todo: Todo) => {
    setEditingId(todo.id)
    setEditTitle(todo.title)
  }

  const handleSaveEdit = async (id: string) => {
    const title = editTitle.trim()
    if (!title) return

    try {
      const updated = await todosTable.update(id, { title })
      setTodos((prev) => prev.map((t) => (t.id === id ? (updated as Todo) : t)))
      setEditingId(null)
      setEditTitle('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update todo')
    }
  }

  const handleCancelEdit = () => {
    setEditingId(null)
    setEditTitle('')
  }

  const handleDelete = async (id: string) => {
    try {
      await todosTable.delete(id)
      setTodos((prev) => prev.filter((t) => t.id !== id))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete todo')
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent, id: string) => {
    if (e.key === 'Enter') handleSaveEdit(id)
    if (e.key === 'Escape') handleCancelEdit()
  }

  return (
    <div className="page">
      <div className="card">
        <div className="header-row">
          <div>
            <h1>Todos</h1>
            <p className="subtitle">
              {config.baseURL}/{config.app}
            </p>
          </div>
          <div className="header-actions">
            <button className="btn-ghost" onClick={() => onNavigate('files')}>
              Files
            </button>
            <button className="btn-ghost" onClick={onLogout}>
              Logout
            </button>
          </div>
        </div>

        <form className="create-form" onSubmit={handleCreate}>
          <input
            type="text"
            value={newTitle}
            onChange={(e) => setNewTitle(e.target.value)}
            placeholder="Add a new todo…"
            className="input-stretch"
          />
          <button type="submit" className="btn-primary" disabled={!newTitle.trim()}>
            Add
          </button>
        </form>

        {error && <p className="error">{error}</p>}

        {loading ? (
          <p className="loading">Loading todos…</p>
        ) : todos.length === 0 ? (
          <p className="empty">No todos yet. Create one above!</p>
        ) : (
          <ul className="todo-list">
            {todos.map((todo) => (
              <li key={todo.id} className={`todo-item ${todo.completed ? 'completed' : ''}`}>
                <input
                  type="checkbox"
                  checked={todo.completed}
                  onChange={() => handleToggle(todo)}
                  className="todo-checkbox"
                />

                {editingId === todo.id ? (
                  <div className="edit-group">
                    <input
                      type="text"
                      value={editTitle}
                      onChange={(e) => setEditTitle(e.target.value)}
                      onKeyDown={(e) => handleKeyDown(e, todo.id)}
                      className="input-stretch"
                      autoFocus
                    />
                    <button className="btn-sm" onClick={() => handleSaveEdit(todo.id)}>
                      Save
                    </button>
                    <button className="btn-sm btn-ghost" onClick={handleCancelEdit}>
                      Cancel
                    </button>
                  </div>
                ) : (
                  <>
                    <span className="todo-title">{todo.title}</span>
                    <div className="todo-actions">
                      <button className="btn-sm btn-ghost" onClick={() => handleStartEdit(todo)}>
                        Edit
                      </button>
                      <button className="btn-sm btn-danger" onClick={() => handleDelete(todo.id)}>
                        Delete
                      </button>
                    </div>
                  </>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
