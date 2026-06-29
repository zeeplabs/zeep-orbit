from __future__ import annotations

import json
from typing import Any, Optional
from urllib.request import Request, urlopen
from urllib.error import HTTPError

from .types import ClientConfig
from .table import TableClient
from .auth import AuthClient
from .files import FilesClient


class OrbitError(Exception):
    def __init__(self, message: str, status: int = 0):
        self.status = status
        super().__init__(message)


class OrbitClient:
    def __init__(self, config: ClientConfig):
        self._config = config
        self.auth = AuthClient(self)
        self.files = FilesClient(self)

    def table(self, name: str) -> TableClient:
        return TableClient(self, name)

    def _url(self, path: str) -> str:
        return f"{self._config.base_url}/{self._config.app}/{path.lstrip('/')}"

    def _request(
        self,
        method: str,
        path: str,
        body: Optional[dict] = None,
        headers: Optional[dict[str, str]] = None,
        authenticated: bool = True,
    ) -> Any:
        url = self._url(path)
        hdrs = headers or {}
        if authenticated:
            hdrs.setdefault("Authorization", f"Bearer {self._config.jwt}")
        if body is not None:
            hdrs.setdefault("Content-Type", "application/json")
            data = json.dumps(body).encode()
        else:
            data = None

        req = Request(url, data=data, headers=hdrs, method=method)
        try:
            with urlopen(req) as resp:
                if resp.status == 204:
                    return None
                return json.loads(resp.read())
        except HTTPError as e:
            try:
                err_body = json.loads(e.read())
                msg = err_body.get("error", f"HTTP {e.code}")
            except Exception:
                msg = f"HTTP {e.code}"
            raise OrbitError(msg, status=e.code) from e
