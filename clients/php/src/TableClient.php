<?php

namespace Zeeplabs\Orbit;

class TableClient
{
    public function __construct(
        private readonly OrbitClient $client,
        private readonly string $table
    ) {}

    public function findMany(
        ?int $limit = null,
        ?int $offset = null,
        ?string $order = null,
        ?array $filters = null
    ): array {
        $params = [];
        if ($limit !== null) $params['limit'] = (string) $limit;
        if ($offset !== null) $params['offset'] = (string) $offset;
        if ($order !== null) $params['order'] = $order;
        if ($filters !== null) $params = array_merge($params, $filters);

        $qs = http_build_query($params);
        $path = $this->table . ($qs ? '/?' . $qs : '');
        return $this->client->request('GET', $path);
    }

    public function findById(string $id): array
    {
        return $this->client->request('GET', "{$this->table}/{$id}");
    }

    public function create(array $data): array
    {
        return $this->client->request('POST', "{$this->table}/", $data);
    }

    public function update(string $id, array $data): array
    {
        return $this->client->request('PATCH', "{$this->table}/{$id}", $data);
    }

    public function replace(string $id, array $data): array
    {
        return $this->client->request('PUT', "{$this->table}/{$id}", $data);
    }

    public function delete(string $id): void
    {
        $this->client->request('DELETE', "{$this->table}/{$id}");
    }
}
