from dataclasses import dataclass, field
from typing import Any, Optional


@dataclass
class ClientConfig:
    base_url: str
    app: str
    jwt: str


@dataclass
class ListResponse:
    data: list[dict[str, Any]]
    count: int
    limit: int
    offset: int


@dataclass
class AuthResponse:
    token: str
    refresh_token: str
    user: dict[str, Any]


@dataclass
class AuthUser:
    id: str
    email: str
    name: Optional[str] = None
    phone: Optional[str] = None
    avatar_url: Optional[str] = None


@dataclass
class FileResponse:
    id: str
    name: str
    size: int
    mime_type: str
    url: str
    created_at: str
