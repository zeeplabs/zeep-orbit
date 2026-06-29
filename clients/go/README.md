# orbit-go

Go client for [Zeep Orbit](https://github.com/zeeplabs/zeep-orbit).

```bash
go get github.com/zeeplabs/orbit-go
```

## Usage

```go
import "github.com/zeeplabs/orbit-go"

client := orbit.New(orbit.ClientConfig{
    BaseURL: "https://orbit.zeeplabs.com",
    App:     "my_app",
    JWT:     "your-jwt-token",
})

// CRUD
rows, err := client.Table("invoices").FindMany(ctx, &orbit.FindManyParams{
    Filters: map[string]string{"status": "eq.pending"},
    Limit:   10,
})

invoice, err := client.Table("invoices").Create(ctx, map[string]any{
    "amount": 150.0,
    "status": "pending",
})

err = client.Table("invoices").Delete(ctx, invoice["id"])

// Auth
resp, err := client.Auth().Login(ctx, orbit.AuthLoginParams{
    Email: "user@example.com", Password: "...",
})

// Files
file, err := client.Files().Upload(ctx, "photo.jpg", reader, "image/jpeg")
url, err := client.Files().SignedURL(ctx, file.ID, 3600)
```
