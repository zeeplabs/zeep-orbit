# zeeplabs-orbit-client

Python client for [Zeep Orbit](https://github.com/zeeplabs/zeep-orbit) — an open-source, self-hosted BaaS platform.

```python
from zeeplabs_orbit_client import OrbitClient, ClientConfig

orbit = OrbitClient(ClientConfig(
    base_url="https://orbit.zeeplabs.com",
    app="my_app",
    jwt="your-jwt-token",
))

# CRUD
rows = orbit.table("invoices").find_many(limit=10)
invoice = orbit.table("invoices").create({"amount": 150.0, "status": "pending"})
orbit.table("invoices").delete(invoice["id"])

# Auth
resp = orbit.auth.login("user@example.com", "password")

# Files
file = orbit.files.upload("photo.jpg", data, "image/jpeg")
url = orbit.files.signed_url(file["id"], ttl=3600)
```

Zero external dependencies — uses only Python standard library.
