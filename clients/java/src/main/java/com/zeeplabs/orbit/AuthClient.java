package com.zeeplabs.orbit;

import com.zeeplabs.orbit.Types.AuthResponse;
import com.zeeplabs.orbit.Types.AuthUser;
import java.util.HashMap;
import java.util.Map;

public class AuthClient {

    private final OrbitClient client;

    AuthClient(OrbitClient client) {
        this.client = client;
    }

    private String path(String p) {
        return "auth/" + p;
    }

    public AuthResponse login(String email, String password) {
        Map<String, String> body = new HashMap<>();
        body.put("email", email);
        body.put("password", password);
        return client.request("POST", path("login"), body, AuthResponse.class, false);
    }

    public AuthResponse register(String email, String password) {
        return register(email, password, null);
    }

    public AuthResponse register(String email, String password, String name) {
        Map<String, String> body = new HashMap<>();
        body.put("email", email);
        body.put("password", password);
        if (name != null) body.put("name", name);
        return client.request("POST", path("register"), body, AuthResponse.class, false);
    }

    public AuthUser me() {
        return client.request("GET", path("me"), null, AuthUser.class);
    }

    public AuthUser updateMe(Map<String, Object> data) {
        return client.request("PUT", path("me"), data, AuthUser.class);
    }

    public void logout() {
        client.request("POST", path("logout"), null, null);
    }
}
