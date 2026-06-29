use crate::client::OrbitClient;
use crate::types::FileResponse;
use reqwest::multipart;

#[derive(Debug)]
pub struct FilesClient<'a> {
    client: &'a OrbitClient,
}

impl<'a> FilesClient<'a> {
    pub fn new(client: &'a OrbitClient) -> Self {
        Self { client }
    }

    fn path(&self, p: &str) -> String {
        format!("files/{}", p.trim_start_matches('/'))
    }

    pub async fn upload(
        &self,
        filename: &str,
        data: Vec<u8>,
        mime_type: &str,
    ) -> Result<FileResponse, reqwest::Error> {
        let part = multipart::Part::bytes(data)
            .file_name(filename.to_string())
            .mime_str(mime_type)
            .unwrap();
        let form = multipart::Form::new().part("file", part);

        let resp = self
            .client
            .http
            .post(self.client.url(&self.path("")))
            .multipart(form)
            .header(
                "Authorization",
                format!("Bearer {}", self.client.config.jwt),
            )
            .send()
            .await?;

        resp.json::<FileResponse>().await
    }

    pub async fn list(&self, limit: i64, offset: i64) -> Result<Vec<FileResponse>, reqwest::Error> {
        let resp = self
            .client
            .http
            .get(self.client.url(&self.path(&format!("?limit={}&offset={}", limit, offset))))
            .headers(self.client.headers())
            .send()
            .await?;
        resp.json::<Vec<FileResponse>>().await
    }

    pub async fn get(&self, id: &str) -> Result<FileResponse, reqwest::Error> {
        let resp = self
            .client
            .http
            .get(self.client.url(&self.path(id)))
            .headers(self.client.headers())
            .send()
            .await?;
        resp.json::<FileResponse>().await
    }

    pub async fn delete(&self, id: &str) -> Result<(), reqwest::Error> {
        self.client
            .http
            .delete(self.client.url(&self.path(id)))
            .headers(self.client.headers())
            .send()
            .await?;
        Ok(())
    }

    pub async fn signed_url(&self, id: &str, ttl: i64) -> Result<String, reqwest::Error> {
        #[derive(serde::Deserialize)]
        struct SignedUrlResp {
            url: String,
        }
        let resp = self
            .client
            .http
            .get(self.client.url(&self.path(&format!("{}/url?ttl={}", id, ttl))))
            .headers(self.client.headers())
            .send()
            .await?
            .json::<SignedUrlResp>()
            .await?;
        Ok(resp.url)
    }
}
