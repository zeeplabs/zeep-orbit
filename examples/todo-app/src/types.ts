export interface Todo {
  id: string
  title: string
  completed: boolean
  created_at: string
  updated_at: string
}

export interface OrbitConfig {
  baseURL: string
  app: string
  jwt?: string
}
