package com.zeeplabs.orbit;

import com.zeeplabs.orbit.Types.FileResponse;
import com.fasterxml.jackson.databind.ObjectMapper;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URI;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.Map;
import java.util.UUID;

public class FilesClient {

    private final OrbitClient client;
    private final ObjectMapper mapper = new ObjectMapper();

    FilesClient(OrbitClient client) {
        this.client = client;
    }

    private String path(String p) {
        return "files/" + p.replaceAll("^/", "");
    }

    public FileResponse upload(String filename, byte[] data, String mimeType) throws Exception {
        String boundary = UUID.randomUUID().toString();
        String header = "--" + boundary + "\r\n"
                + "Content-Disposition: form-data; name=\"file\"; filename=\"" + filename + "\"\r\n"
                + "Content-Type: " + mimeType + "\r\n\r\n";
        String footer = "\r\n--" + boundary + "--\r\n";

        byte[] headerBytes = header.getBytes(StandardCharsets.UTF_8);
        byte[] footerBytes = footer.getBytes(StandardCharsets.UTF_8);
        byte[] body = new byte[headerBytes.length + data.length + footerBytes.length];
        System.arraycopy(headerBytes, 0, body, 0, headerBytes.length);
        System.arraycopy(data, 0, body, headerBytes.length, data.length);
        System.arraycopy(footerBytes, 0, body, headerBytes.length + data.length, footerBytes.length);

        URL url = new URI(client.url(path(""))).toURL();
        HttpURLConnection conn = (HttpURLConnection) url.openConnection();
        conn.setRequestMethod("POST");
        conn.setRequestProperty("Authorization", "Bearer " + client.config.jwt);
        conn.setRequestProperty("Content-Type", "multipart/form-data; boundary=" + boundary);
        conn.setDoOutput(true);

        try (OutputStream os = conn.getOutputStream()) {
            os.write(body);
        }

        int status = conn.getResponseCode();
        byte[] resp = (status >= 400 ? conn.getErrorStream() : conn.getInputStream()).readAllBytes();
        String raw = new String(resp, StandardCharsets.UTF_8);

        if (status >= 400) {
            Map<String, Object> err = mapper.readValue(raw, Map.class);
            throw new RuntimeException((String) err.getOrDefault("error", "HTTP " + status));
        }

        return mapper.readValue(raw, FileResponse.class);
    }

    public FileResponse[] list(int limit, int offset) {
        return client.request("GET", path("?limit=" + limit + "&offset=" + offset), null, FileResponse[].class);
    }

    public FileResponse get(String id) {
        return client.request("GET", path(id), null, FileResponse.class);
    }

    public void delete(String id) {
        client.request("DELETE", path(id), null, null);
    }

    @SuppressWarnings("unchecked")
    public String signedURL(String id, int ttl) {
        Map<String, Object> resp = client.request("GET", path(id + "/url?ttl=" + ttl), null, Map.class);
        return (String) resp.get("url");
    }
}
