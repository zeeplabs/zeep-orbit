package server

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type errorResponse struct {
	Error string `json:"error"`
}

// writeJSON serializa v como JSON e envia com o status code dado.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError envia {"error": msg} com o status code dado.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

// sanitizeRow converte tipos pgx não-serializáveis em valores JSON-friendly.
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
