package com.zeeplabs.orbit;

import com.zeeplabs.orbit.Types.ListResponse;
import java.util.HashMap;
import java.util.Map;

public class TableClient {

    private final OrbitClient client;
    private final String table;

    TableClient(OrbitClient client, String table) {
        this.client = client;
        this.table = table;
    }

    public ListResponse findMany(Integer limit, Integer offset, String order, Map<String, String> filters) {
        Map<String, String> params = new HashMap<>();
        if (limit != null) params.put("limit", String.valueOf(limit));
        if (offset != null) params.put("offset", String.valueOf(offset));
        if (order != null) params.put("order", order);
        if (filters != null) params.putAll(filters);

        String qs = client.buildQuery(params);
        return client.request("GET", table + "/" + qs, null, ListResponse.class);
    }

    public Map<String, Object> findById(String id) {
        return client.request("GET", table + "/" + id, null, Map.class);
    }

    public Map<String, Object> create(Map<String, Object> data) {
        return client.request("POST", table + "/", data, Map.class);
    }

    public Map<String, Object> update(String id, Map<String, Object> data) {
        return client.request("PATCH", table + "/" + id, data, Map.class);
    }

    public Map<String, Object> replace(String id, Map<String, Object> data) {
        return client.request("PUT", table + "/" + id, data, Map.class);
    }

    public void delete(String id) {
        client.request("DELETE", table + "/" + id, null, null);
    }
}
