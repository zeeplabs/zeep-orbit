---
sidebar_position: 10
---

# SDK Clients

Official clients for all major languages. Same API design across all.

## TypeScript

```bash
npm install @zeeptech/orbit-client
```

```typescript
import { OrbitClient } from '@zeeptech/orbit-client'

const orbit = new OrbitClient({
  baseURL: 'https://orbit.zeeplabs.com',
  app: 'myapp',
  jwt: 'your-jwt-token',
})

// CRUD
const rows = await orbit.table('invoices').findMany({ limit: 10 })
await orbit.table('invoices').create({ amount: 150, status: 'pending' })

// Auth
const { token } = await orbit.auth.login({ email: '...', password: '...' })
const me = await orbit.auth.me()

// Files
const file = await orbit.files.upload(fileInput.files[0])
const url = await orbit.files.signedURL(file.id)
```

## Go

```bash
go get github.com/zeeplabs/orbit-go
```

```go
import "github.com/zeeplabs/orbit-go"

client := orbit.New(orbit.ClientConfig{
    BaseURL: "https://orbit.zeeplabs.com",
    App:     "myapp",
    JWT:     "your-jwt-token",
})

rows, _ := client.Table("invoices").FindMany(ctx, &orbit.FindManyParams{Limit: 10})
```

## Python

```bash
pip install zeeplabs-orbit-client
```

```python
from zeeplabs_orbit_client import OrbitClient, ClientConfig

orbit = OrbitClient(ClientConfig("https://orbit.zeeplabs.com", "myapp", "jwt"))
rows = orbit.table("invoices").find_many(limit=10)
```

## Rust

```toml
[dependencies]
orbit-client = "0.1"
```

```rust
use orbit_client::OrbitClient;

let orbit = OrbitClient::new(cfg);
let rows = orbit.table("invoices").find_many(Some(10), None, None, None).await?;
```

## Java

```xml
<dependency>
    <groupId>com.zeeplabs</groupId>
    <artifactId>orbit-client</artifactId>
    <version>0.1.0</version>
</dependency>
```

```java
OrbitClient orbit = new OrbitClient(new ClientConfig(baseURL, "myapp", jwt));
ListResponse resp = orbit.table("invoices").findMany(10, 0, null, null);
```

## PHP

```bash
composer require zeeplabs/orbit-client
```

```php
$orbit = new Zeeplabs\Orbit\OrbitClient($baseURL, 'myapp', $jwt);
$rows = $orbit->table('invoices')->findMany(limit: 10);
```

## Source

All client source code is in the [`clients/`](https://github.com/zeeplabs/zeep-orbit/tree/main/clients) directory of the main repository.
