from __future__ import annotations

from typing import Any, Optional
from urllib.parse import urlencode

from .types import ListResponse


class TableClient:
    def __init__(self, client, table: str):
        self._client = client
        self._table = table

    def find_many(
        self,
        limit: Optional[int] = None,
        offset: Optional[int] = 0,
        order: Optional[str] = None,
        filters: Optional[dict[str, str]] = None,
    ) -> ListResponse:
        params = {}
        if limit is not None:
            params["limit"] = str(limit)
        if offset:
            params["offset"] = str(offset)
        if order:
            params["order"] = order
        if filters:
            params.update(filters)
        qs = urlencode(params)
        path = self._table
        if qs:
            path = f"{self._table}/?{qs}"
        data = self._client._request("GET", path)
        return ListResponse(**data)

    def find_by_id(self, id: str) -> dict[str, Any]:
        return self._client._request("GET", f"{self._table}/{id}")

    def create(self, data: dict[str, Any]) -> dict[str, Any]:
        return self._client._request("POST", f"{self._table}/", body=data)

    def update(self, id: str, data: dict[str, Any]) -> dict[str, Any]:
        return self._client._request("PATCH", f"{self._table}/{id}", body=data)

    def replace(self, id: str, data: dict[str, Any]) -> dict[str, Any]:
        return self._client._request("PUT", f"{self._table}/{id}", body=data)

    def delete(self, id: str) -> None:
        self._client._request("DELETE", f"{self._table}/{id}")
