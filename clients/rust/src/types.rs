use serde::{Deserialize, Serialize};
use std::collections::HashMap;

#[derive(Debug, Clone)]
pub struct ClientConfig {
    pub base_url: String,
    pub app: String,
    pub jwt: String,
}

#[derive(Debug, Deserialize)]
pub struct ListResponse {
    pub data: Vec<HashMap<String, serde_json::Value>>,
    pub count: i64,
    pub limit: i64,
    pub offset: i64,
}

#[derive(Debug, Serialize)]
pub struct AuthLoginParams {
    pub email: String,
    pub password: String,
}

#[derive(Debug, Serialize)]
pub struct AuthRegisterParams {
    pub email: String,
    pub password: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct AuthResponse {
    pub token: String,
    pub refresh_token: String,
    pub user: AuthUser,
}

#[derive(Debug, Deserialize)]
pub struct AuthUser {
    pub id: String,
    pub email: String,
    #[serde(default)]
    pub name: Option<String>,
    #[serde(default)]
    pub phone: Option<String>,
    #[serde(default)]
    pub avatar_url: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct FileResponse {
    pub id: String,
    pub name: String,
    pub size: i64,
    pub mime_type: String,
    pub url: String,
    pub created_at: String,
}
