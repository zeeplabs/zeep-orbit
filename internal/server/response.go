package server

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type errorResponse struct {
	Error string `json:"error"`
}

// writeJSON serializes v as JSON and sends it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError sends {"error": msg} with the given status code.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

// [16]byte (UUID do pgx v5) → string "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx".
func sanitizeRow(row map[string]any) map[string]any {
	for k, v := range row {
		if b, ok := v.([16]byte); ok {
			row[k] = fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
				b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
		}
	}
	return row
}

// sanitizeRows aplica sanitizeRow a cada elemento da slice.
func sanitizeRows(rows []map[string]any) []map[string]any {
	for i, r := range rows {
		rows[i] = sanitizeRow(r)
	}
	return rows
}
