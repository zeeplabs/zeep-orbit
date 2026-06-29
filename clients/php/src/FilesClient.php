<?php

namespace Zeeplabs\Orbit;

class FilesClient
{
    public function __construct(
        private readonly OrbitClient $client
    ) {}

    private function path(string $p): string
    {
        return 'files/' . ltrim($p, '/');
    }

    public function upload(string $filename, string $data, string $mimeType): array
    {
        $boundary = bin2hex(random_bytes(16));
        $header = "--{$boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"{$filename}\"\r\nContent-Type: {$mimeType}\r\n\r\n";
        $footer = "\r\n--{$boundary}--\r\n";
        $body = $header . $data . $footer;

        $ch = curl_init();
        curl_setopt_array($ch, [
            CURLOPT_URL => $this->client->url($this->path('')),
            CURLOPT_RETURNTRANSFER => true,
            CURLOPT_CUSTOMREQUEST => 'POST',
            CURLOPT_POSTFIELDS => $body,
            CURLOPT_HTTPHEADER => [
                'Authorization: Bearer ' . $this->client->jwt,
                'Content-Type: multipart/form-data; boundary=' . $boundary,
            ],
        ]);

        $response = curl_exec($ch);
        $status = curl_getinfo($ch, CURLINFO_HTTP_CODE);
        curl_close($ch);

        $data = json_decode($response, true);

        if ($status >= 400) {
            $msg = $data['error'] ?? 'HTTP ' . $status;
            throw new OrbitException($msg, $status);
        }

        return $data;
    }

    public function list(int $limit = 50, int $offset = 0): array
    {
        return $this->client->request('GET', $this->path("?limit={$limit}&offset={$offset}"));
    }

    public function get(string $id): array
    {
        return $this->client->request('GET', $this->path($id));
    }

    public function delete(string $id): void
    {
        $this->client->request('DELETE', $this->path($id));
    }

    public function signedURL(string $id, int $ttl = 3600): string
    {
        $resp = $this->client->request('GET', $this->path("{$id}/url?ttl={$ttl}"));
        return $resp['url'];
    }
}
