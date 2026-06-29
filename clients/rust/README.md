# zeep-orbit-client

Rust client for [Zeep Orbit](https://github.com/zeeplabs/zeep-orbit) — an open-source, self-hosted BaaS platform.

```rust
use orbit_client::OrbitClient, ClientConfig};

#[tokio::main]
async fn main() {
    let cfg = ClientConfig {
        base_url: "https://orbit.zeeplabs.com".into(),
        app: "my_app".into(),
        jwt: "your-jwt-token".into(),
    };
    let orbit = OrbitClient::new(cfg);

    // CRUD
    let rows = orbit.table("invoices")
        .find_many(Some(10), None, None, None)
        .await
        .unwrap();

    // Auth
    let resp = orbit.auth()
        .login("user@example.com", "password")
        .await
        .unwrap();

    // Files
    let file = orbit.files()
        .upload("photo.jpg", data, "image/jpeg")
        .await
        .unwrap();
}
```

Dependencies: `reqwest`, `serde`, `serde_json`, `tokio`.
