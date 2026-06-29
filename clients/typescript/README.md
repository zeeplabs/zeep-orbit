# @zeeplabs/orbit-client

TypeScript client for [Zeep Orbit](https://github.com/zeeplabs/zeep-orbit).

```bash
npm install @zeeplabs/orbit-client
```

## Usage

```typescript
import { OrbitClient } from '@zeeplabs/orbit-client'

const orbit = new OrbitClient({
  baseURL: 'https://orbit.zeeplabs.com',
  app: 'my_app',
  jwt: 'your-jwt-token',
})

// CRUD
const rows = await orbit.table('invoices').findMany({
  filters: { status: 'eq.pending' },
  limit: 10,
})

const invoice = await orbit.table('invoices').create({
  amount: 150.0,
  status: 'pending',
})

await orbit.table('invoices').update(invoice.id, { status: 'paid' })
await orbit.table('invoices').delete(invoice.id)

// Auth
const { token } = await orbit.auth.login({ email: 'user@example.com', password: '...' })
const me = await orbit.auth.me()

// Files
const file = await orbit.files.upload(fileInput.files[0])
const url = await orbit.files.signedURL(file.id)
```
