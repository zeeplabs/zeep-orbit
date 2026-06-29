from __future__ import annotations

from typing import Optional
from urllib.request import Request

from .types import FileResponse


class FilesClient:
    def __init__(self, client):
        self._client = client

    def _path(self, p: str) -> str:
        return f"files/{p.lstrip('/')}"

    def upload(self, filename: str, data: bytes, mime_type: str) -> FileResponse:
        import uuid
        boundary = uuid.uuid4().hex
        header = f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"{filename}\"\r\nContent-Type: {mime_type}\r\n\r\n"
        footer = f"\r\n--{boundary}--\r\n"
        body = header.encode() + data + footer.encode()
        content_type = f"multipart/form-data; boundary={boundary}"

        url = self._client._url(self._path(""))
        req = Request(
            url,
            data=body,
            headers={
                "Authorization": f"Bearer {self._client._config.jwt}",
                "Content-Type": content_type,
            },
            method="POST",
        )
        from urllib.request import urlopen
        with urlopen(req) as resp:
            import json
            result = json.loads(resp.read())
            return FileResponse(**result)

    def list(self, limit: int = 50, offset: int = 0) -> list[FileResponse]:
        data = self._client._request("GET", self._path(f"?limit={limit}&offset={offset}"))
        return [FileResponse(**f) for f in data]

    def get(self, id: str) -> FileResponse:
        data = self._client._request("GET", self._path(id))
        return FileResponse(**data)

    def delete(self, id: str) -> None:
        self._client._request("DELETE", self._path(id))

    def signed_url(self, id: str, ttl: int = 3600) -> str:
        data = self._client._request("GET", self._path(f"{id}/url?ttl={ttl}"))
        return data["url"]
