# Official Orbit client SDKs

All 6 clients wrap the REST API in `rest-api.md` — same auth model (per-app JWT), same table/files/auth semantics. Pick by language; don't hand-roll HTTP calls if an official client exists for the target language.

## TypeScript — `@zeeptech/orbit-client`

```bash
npm install @zeeptech/orbit-client
```

```typescript
import { OrbitClient } from '@zeeptech/orbit-client'

const orbit = new OrbitClient({
  baseURL: 'https://orbit.zeeplabs.com',
  app: 'my_app',
  jwt: 'your-jwt-token',
})

const rows = await orbit.table('invoices').findMany({ filters: { status: 'eq.pending' }, limit: 10 })
const invoice = await orbit.table('invoices').create({ amount: 150.0, status: 'pending' })
await orbit.table('invoices').update(invoice.id, { status: 'paid' })
await orbit.table('invoices').delete(invoice.id)

const { token } = await orbit.auth.login({ email: 'user@example.com', password: '...' })
const me = await orbit.auth.me()

const file = await orbit.files.upload(fileInput.files[0])
const url = await orbit.files.signedURL(file.id)
```

## Python — `zeeplabs-orbit-client`

Zero external dependencies (stdlib only).

```python
from zeeplabs_orbit_client import OrbitClient, ClientConfig

orbit = OrbitClient(ClientConfig(base_url="https://orbit.zeeplabs.com", app="my_app", jwt="your-jwt-token"))

rows = orbit.table("invoices").find_many(limit=10)
invoice = orbit.table("invoices").create({"amount": 150.0, "status": "pending"})
orbit.table("invoices").delete(invoice["id"])

resp = orbit.auth.login("user@example.com", "password")

file = orbit.files.upload("photo.jpg", data, "image/jpeg")
url = orbit.files.signed_url(file["id"], ttl=3600)
```

## Go — `github.com/zeeplabs/orbit-go`

```bash
go get github.com/zeeplabs/orbit-go
```

```go
import "github.com/zeeplabs/orbit-go"

client := orbit.New(orbit.ClientConfig{BaseURL: "https://orbit.zeeplabs.com", App: "my_app", JWT: "your-jwt-token"})

rows, err := client.Table("invoices").FindMany(ctx, &orbit.FindManyParams{
    Filters: map[string]string{"status": "eq.pending"},
    Limit:   10,
})

invoice, err := client.Table("invoices").Create(ctx, map[string]any{"amount": 150.0, "status": "pending"})
```

## Rust — `zeep-orbit-client` (crate `orbit_client`)

```rust
use orbit_client::{OrbitClient, ClientConfig};

let cfg = ClientConfig {
    base_url: "https://orbit.zeeplabs.com".into(),
    app: "my_app".into(),
    jwt: "your-jwt-token".into(),
};
let orbit = OrbitClient::new(cfg);

let rows = orbit.table("invoices").find_many(Some(10), None, None, None).await?;
let resp = orbit.auth().login("user@example.com", "password").await?;
let file = orbit.files().upload(/* ... */).await?;
```

## Java — `com.zeeplabs:orbit-client`

Maven:
```xml
<dependency>
  <groupId>com.zeeplabs</groupId>
  <artifactId>orbit-client</artifactId>
  <version>0.1.0</version>
</dependency>
```

## PHP — `zeeplabs/orbit-client`

```bash
composer require zeeplabs/orbit-client
```
Requires PHP >= 8.1 and the `curl` extension.

## Which client to pick

Match the target runtime, not preference — if the frontend is a Node/Vite/React app, use the TypeScript client; if the script or service is Python/Go/Rust/Java/PHP, use that client. Don't introduce an HTTP-from-scratch integration when an official client covers the language — you lose auth-token handling, retry/error shaping, and filter-string construction that the client already gets right.
