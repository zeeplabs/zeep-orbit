from __future__ import annotations

from typing import Any, Optional

from .types import AuthResponse, AuthUser


class AuthClient:
    def __init__(self, client):
        self._client = client

    def _path(self, p: str) -> str:
        return f"auth/{p}"

    def login(self, email: str, password: str) -> AuthResponse:
        data = self._client._request(
            "POST", self._path("login"),
            body={"email": email, "password": password},
            authenticated=False,
        )
        return AuthResponse(**data)

    def register(self, email: str, password: str, name: Optional[str] = None) -> AuthResponse:
        body: dict[str, Any] = {"email": email, "password": password}
        if name:
            body["name"] = name
        data = self._client._request(
            "POST", self._path("register"),
            body=body,
            authenticated=False,
        )
        return AuthResponse(**data)

    def me(self) -> AuthUser:
        data = self._client._request("GET", self._path("me"))
        return AuthUser(**data)

    def update_me(self, data: dict[str, Any]) -> AuthUser:
        resp = self._client._request("PUT", self._path("me"), body=data)
        return AuthUser(**resp)

    def logout(self) -> None:
        self._client._request("POST", self._path("logout"))
