use crate::auth::AuthClient;
use crate::files::FilesClient;
use crate::table::TableClient;
use crate::types::ClientConfig;
use reqwest::header::{HeaderMap, HeaderValue, AUTHORIZATION, CONTENT_TYPE};

#[derive(Debug)]
pub struct OrbitClient {
    pub config: ClientConfig,
    pub http: reqwest::Client,
}

impl OrbitClient {
    pub fn new(config: ClientConfig) -> Self {
        Self {
            http: reqwest::Client::new(),
            config,
        }
    }

    pub fn table(&self, name: &str) -> TableClient<'_> {
        TableClient::new(self, name)
    }

    pub fn auth(&self) -> AuthClient<'_> {
        AuthClient::new(self)
    }

    pub fn files(&self) -> FilesClient<'_> {
        FilesClient::new(self)
    }

    pub fn url(&self, path: &str) -> String {
        format!(
            "{}/{}/{}",
            self.config.base_url.trim_end_matches('/'),
            self.config.app,
            path.trim_start_matches('/')
        )
    }

    pub fn headers(&self) -> HeaderMap {
        let mut h = HeaderMap::new();
        h.insert(
            AUTHORIZATION,
            HeaderValue::try_from(format!("Bearer {}", self.config.jwt)).unwrap(),
        );
        h.insert(CONTENT_TYPE, HeaderValue::from_static("application/json"));
        h
    }
}
