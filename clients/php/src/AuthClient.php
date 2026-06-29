<?php

namespace Zeeplabs\Orbit;

class AuthClient
{
    public function __construct(
        private readonly OrbitClient $client
    ) {}

    private function path(string $p): string
    {
        return "auth/{$p}";
    }

    public function login(string $email, string $password): array
    {
        return $this->client->request('POST', $this->path('login'), [
            'email' => $email,
            'password' => $password,
        ], authenticated: false);
    }

    public function register(string $email, string $password, ?string $name = null): array
    {
        $body = ['email' => $email, 'password' => $password];
        if ($name !== null) {
            $body['name'] = $name;
        }
        return $this->client->request('POST', $this->path('register'), $body, authenticated: false);
    }

    public function me(): array
    {
        return $this->client->request('GET', $this->path('me'));
    }

    public function updateMe(array $data): array
    {
        return $this->client->request('PUT', $this->path('me'), $data);
    }

    public function logout(): void
    {
        $this->client->request('POST', $this->path('logout'));
    }
}
