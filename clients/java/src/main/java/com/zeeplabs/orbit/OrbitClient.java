package com.zeeplabs.orbit;

import com.fasterxml.jackson.databind.ObjectMapper;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URI;
import java.net.URL;
import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;
import java.util.Map;

public class OrbitClient {

    final Types.ClientConfig config;
    private final ObjectMapper mapper;
    public final AuthClient auth;
    public final FilesClient files;

    public OrbitClient(Types.ClientConfig config) {
        this.config = config;
        this.mapper = new ObjectMapper();
        this.auth = new AuthClient(this);
        this.files = new FilesClient(this);
    }

    public TableClient table(String name) {
        return new TableClient(this, name);
    }

    public String url(String path) {
        return config.baseURL.replaceAll("/$", "") + "/" + config.app + "/" + path.replaceAll("^/", "");
    }

    HttpURLConnection connection(String method, String path) throws Exception {
        return connection(method, path, null, true);
    }

    HttpURLConnection connection(String method, String path, Object body, boolean authenticated) throws Exception {
        URL url = new URI(url(path)).toURL();
        HttpURLConnection conn = (HttpURLConnection) url.openConnection();
        conn.setRequestMethod(method);
        conn.setRequestProperty("Content-Type", "application/json");
        if (authenticated) {
            conn.setRequestProperty("Authorization", "Bearer " + config.jwt);
        }
        conn.setDoOutput(body != null);

        if (body != null) {
            String json = mapper.writeValueAsString(body);
            try (OutputStream os = conn.getOutputStream()) {
                os.write(json.getBytes(StandardCharsets.UTF_8));
            }
        }

        return conn;
    }

    @SuppressWarnings("unchecked")
    <T> T request(String method, String path, Object body, Class<T> type) throws OrbitException {
        return request(method, path, body, type, true);
    }

    @SuppressWarnings("unchecked")
    <T> T request(String method, String path, Object body, Class<T> type, boolean authenticated) throws OrbitException {
        try {
            HttpURLConnection conn = connection(method, path, body, authenticated);
            int status = conn.getResponseCode();

            if (status == 204) return null;

            byte[] resp;
            if (status >= 400) {
                resp = conn.getErrorStream().readAllBytes();
            } else {
                resp = conn.getInputStream().readAllBytes();
            }

            String raw = new String(resp, StandardCharsets.UTF_8);

            if (status >= 400) {
                Map<String, Object> err = mapper.readValue(raw, Map.class);
                String msg = (String) err.getOrDefault("error", "HTTP " + status);
                throw new OrbitException(msg, status);
            }

            if (type == null) return null;
            return mapper.readValue(raw, type);
        } catch (OrbitException e) {
            throw e;
        } catch (Exception e) {
            throw new OrbitException(e.getMessage(), 0);
        }
    }

    String buildQuery(Map<String, String> params) {
        if (params == null || params.isEmpty()) return "";
        StringBuilder sb = new StringBuilder("?");
        for (var entry : params.entrySet()) {
            if (sb.length() > 1) sb.append("&");
            sb.append(URLEncoder.encode(entry.getKey(), StandardCharsets.UTF_8));
            sb.append("=");
            sb.append(URLEncoder.encode(entry.getValue(), StandardCharsets.UTF_8));
        }
        return sb.toString();
    }
}
