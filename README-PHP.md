# zeeplabs/orbit-client

PHP client for [Zeep Orbit](https://github.com/zeeplabs/zeep-orbit) — an open-source, self-hosted BaaS platform.

```php
$orbit = new Zeeplabs\Orbit\OrbitClient(
    baseURL: 'https://orbit.zeeplabs.com',
    app: 'my_app',
    jwt: 'your-jwt-token',
);

// CRUD
$rows = $orbit->table('invoices')->findMany(limit: 10);

$invoice = $orbit->table('invoices')->create([
    'amount' => 150.0,
    'status' => 'pending',
]);

$orbit->table('invoices')->delete($invoice['id']);

// Auth
$resp = $orbit->auth->login('user@example.com', 'password');

// Files
$file = $orbit->files->upload('photo.jpg', $data, 'image/jpeg');
$url = $orbit->files->signedURL($file['id'], ttl: 3600);
```

Requires PHP 8.1+ with `ext-curl` and `ext-json`.
