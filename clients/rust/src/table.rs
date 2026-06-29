use crate::client::OrbitClient;
use crate::types::ListResponse;
use std::collections::HashMap;

#[derive(Debug)]
pub struct TableClient<'a> {
    client: &'a OrbitClient,
    table: String,
}

impl<'a> TableClient<'a> {
    pub fn new(client: &'a OrbitClient, table: &str) -> Self {
        Self {
            client,
            table: table.to_string(),
        }
    }

    pub async fn find_many(
        &self,
        limit: Option<i64>,
        offset: Option<i64>,
        order: Option<&str>,
        filters: Option<HashMap<String, String>>,
    ) -> Result<ListResponse, reqwest::Error> {
        let mut params = Vec::new();
        if let Some(l) = limit {
            params.push(format!("limit={}", l));
        }
        if let Some(o) = offset {
            params.push(format!("offset={}", o));
        }
        if let Some(o) = order {
            params.push(format!("order={}", o));
        }
        if let Some(f) = filters {
            for (k, v) in f {
                params.push(format!("{}={}", k, v));
            }
        }
        let qs = params.join("&");
        let path = if qs.is_empty() {
            self.table.clone()
        } else {
            format!("{}/?{}", self.table, qs)
        };
        self.client
            .http
            .get(self.client.url(&path))
            .headers(self.client.headers())
            .send()
            .await?
            .json::<ListResponse>()
            .await
    }

    pub async fn find_by_id(&self, id: &str) -> Result<HashMap<String, serde_json::Value>, reqwest::Error> {
        self.client
            .http
            .get(self.client.url(&format!("{}/{}", self.table, id)))
            .headers(self.client.headers())
            .send()
            .await?
            .json()
            .await
    }

    pub async fn create(
        &self,
        data: HashMap<String, serde_json::Value>,
    ) -> Result<HashMap<String, serde_json::Value>, reqwest::Error> {
        self.client
            .http
            .post(self.client.url(&format!("{}/", self.table)))
            .headers(self.client.headers())
            .json(&data)
            .send()
            .await?
            .json()
            .await
    }

    pub async fn update(
        &self,
        id: &str,
        data: HashMap<String, serde_json::Value>,
    ) -> Result<HashMap<String, serde_json::Value>, reqwest::Error> {
        self.client
            .http
            .patch(self.client.url(&format!("{}/{}", self.table, id)))
            .headers(self.client.headers())
            .json(&data)
            .send()
            .await?
            .json()
            .await
    }

    pub async fn delete(&self, id: &str) -> Result<(), reqwest::Error> {
        self.client
            .http
            .delete(self.client.url(&format!("{}/{}", self.table, id)))
            .headers(self.client.headers())
            .send()
            .await?;
        Ok(())
    }
}
