use crate::client::OrbitClient;
use crate::types::{AuthLoginParams, AuthRegisterParams, AuthResponse, AuthUser};
use std::collections::HashMap;

#[derive(Debug)]
pub struct AuthClient<'a> {
    client: &'a OrbitClient,
}

impl<'a> AuthClient<'a> {
    pub fn new(client: &'a OrbitClient) -> Self {
        Self { client }
    }

    fn path(&self, p: &str) -> String {
        format!("auth/{}", p)
    }

    pub async fn login(&self, email: &str, password: &str) -> Result<AuthResponse, reqwest::Error> {
        self.client
            .http
            .post(self.client.url(&self.path("login")))
            .json(&AuthLoginParams {
                email: email.to_string(),
                password: password.to_string(),
            })
            .send()
            .await?
            .json::<AuthResponse>()
            .await
    }

    pub async fn register(
        &self,
        email: &str,
        password: &str,
        name: Option<&str>,
    ) -> Result<AuthResponse, reqwest::Error> {
        self.client
            .http
            .post(self.client.url(&self.path("register")))
            .json(&AuthRegisterParams {
                email: email.to_string(),
                password: password.to_string(),
                name: name.map(|n| n.to_string()),
            })
            .send()
            .await?
            .json::<AuthResponse>()
            .await
    }

    pub async fn me(&self) -> Result<AuthUser, reqwest::Error> {
        self.client
            .http
            .get(self.client.url(&self.path("me")))
            .headers(self.client.headers())
            .send()
            .await?
            .json::<AuthUser>()
            .await
    }

    pub async fn update_me(&self, data: HashMap<String, serde_json::Value>) -> Result<AuthUser, reqwest::Error> {
        self.client
            .http
            .put(self.client.url(&self.path("me")))
            .headers(self.client.headers())
            .json(&data)
            .send()
            .await?
            .json::<AuthUser>()
            .await
    }

    pub async fn logout(&self) -> Result<(), reqwest::Error> {
        self.client
            .http
            .post(self.client.url(&self.path("logout")))
            .headers(self.client.headers())
            .send()
            .await?;
        Ok(())
    }
}
