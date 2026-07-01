# Zeep Orbit — Todo Example

A simple React + Vite todo app that demonstrates how to connect to a **Zeep Orbit** instance using the official TypeScript SDK (`@zeeptech/orbit-client`).

## Features

- **Connect** — point to any Orbit instance and app name
- **Auth** — register and sign in via `client.auth`
- **CRUD** — create, list, update (inline edit), and delete todos via `client.table("todos")`

## Prerequisites

- A running Zeep Orbit instance (see the [root README](../../README.md))
- An app with a `todos` table configured. Example `apps.yaml`:

```yaml
apps:
  - name: my-app
    tables:
      - name: todos
        columns:
          - name: title
            type: text
            required: true
          - name: completed
            type: boolean
            default: false
        indexes:
          - columns: [created_at]
```

Run `zeep-orbit apply -f apps.yaml` to provision the table.

## Getting Started

```bash
cd examples/todo-app
npm install
npm run dev
```

Open the URL shown by Vite (default `http://localhost:5173`).

### Steps in the app

1. Enter your Orbit **Base URL** (e.g. `http://localhost:8080`) and **App Name**
2. **Register** a new account or **Sign in**
3. Start adding, toggling, editing, and deleting todos

## How it works

Once connected, every operation uses the same pattern:

```ts
import { OrbitClient } from '@zeeptech/orbit-client'

const client = new OrbitClient({
  baseURL: 'http://localhost:8080',
  app: 'my-app',
  jwt: '<token>',
})

const todos = client.table('todos')

// Create
await todos.create({ title: 'Hello Orbit', completed: false })

// List
const { data } = await todos.findMany({ order: 'created_at:desc' })

// Update
await todos.update(id, { completed: true })

// Delete
await todos.delete(id)
```

No backend code needed — Orbit auto-generates the REST API from your table config.
