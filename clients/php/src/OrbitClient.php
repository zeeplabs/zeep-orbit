<?php

namespace Zeeplabs\Orbit;

class OrbitClient
{
    public readonly AuthClient $auth;
    public readonly FilesClient $files;

    public function __construct(
        public readonly string $baseURL,
        public readonly string $app,
        public readonly string $jwt
    ) {
        $this->auth = new AuthClient($this);
        $this->files = new FilesClient($this);
    }

    public function table(string $name): TableClient
    {
        return new TableClient($this, $name);
    }

    public function url(string $path): string
    {
        return rtrim($this->baseURL, '/') . '/' . $this->app . '/' . ltrim($path, '/');
    }

    public function request(
        string $method,
        string $path,
        ?array $body = null,
        bool $authenticated = true
    ): mixed {
        $ch = curl_init();

        curl_setopt_array($ch, [
            CURLOPT_URL => $this->url($path),
            CURLOPT_RETURNTRANSFER => true,
            CURLOPT_CUSTOMREQUEST => $method,
            CURLOPT_HTTPHEADER => [],
        ]);

        $headers = ['Content-Type: application/json'];

        if ($authenticated) {
            $headers[] = 'Authorization: Bearer ' . $this->jwt;
        }

        if ($body !== null) {
            curl_setopt($ch, CURLOPT_POSTFIELDS, json_encode($body));
        }

        curl_setopt($ch, CURLOPT_HTTPHEADER, $headers);

        $response = curl_exec($ch);
        $status = curl_getinfo($ch, CURLINFO_HTTP_CODE);
        curl_close($ch);

        if ($status === 204) {
            return null;
        }

        $data = json_decode($response, true);

        if ($status >= 400) {
            $msg = $data['error'] ?? 'HTTP ' . $status;
            throw new OrbitException($msg, $status);
        }

        return $data;
    }
}
