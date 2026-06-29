package com.zeeplabs.orbit;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.annotation.JsonProperty;
import java.util.List;
import java.util.Map;

public class Types {

    public static class ClientConfig {
        public String baseURL;
        public String app;
        public String jwt;

        public ClientConfig() {}

        public ClientConfig(String baseURL, String app, String jwt) {
            this.baseURL = baseURL;
            this.app = app;
            this.jwt = jwt;
        }
    }

    @JsonIgnoreProperties(ignoreUnknown = true)
    public static class ListResponse {
        public List<Map<String, Object>> data;
        public long count;
        public long limit;
        public long offset;
    }

    @JsonIgnoreProperties(ignoreUnknown = true)
    public static class AuthResponse {
        public String token;
        @JsonProperty("refresh_token")
        public String refreshToken;
        public Map<String, Object> user;
    }

    @JsonIgnoreProperties(ignoreUnknown = true)
    public static class AuthUser {
        public String id;
        public String email;
        public String name;
        public String phone;
        @JsonProperty("avatar_url")
        public String avatarUrl;
    }

    @JsonIgnoreProperties(ignoreUnknown = true)
    public static class FileResponse {
        public String id;
        public String name;
        public long size;
        @JsonProperty("mime_type")
        public String mimeType;
        public String url;
        @JsonProperty("created_at")
        public String createdAt;
    }
}
