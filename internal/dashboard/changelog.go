package dashboard

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

type ChangelogHandler struct {
	pool *db.Pool
}

func NewChangelogHandler(pool *db.Pool) *ChangelogHandler {
	return &ChangelogHandler{pool: pool}
}

func (h *ChangelogHandler) List(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	_ = user

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	entries, total, err := ListChangelogEntries(r.Context(), h.pool, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"entries": entries,
		"total":   total,
	})
}

type changelogEntryBody struct {
	Version     string `json:"version"`
	ReleaseDate string `json:"release_date"`
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	Sections    string `json:"sections"`
	Published   *bool  `json:"published"`
}

func (h *ChangelogHandler) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	var body changelogEntryBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if body.Version == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "version is required"})
		return
	}
	if body.ReleaseDate == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "release_date is required"})
		return
	}

	published := true
	if body.Published != nil {
		published = *body.Published
	}

	entry := &ChangelogEntry{
		Version:     body.Version,
		ReleaseDate: body.ReleaseDate,
		Title:       body.Title,
		Summary:     body.Summary,
		Sections:    body.Sections,
		Published:   published,
	}

	if err := CreateChangelogEntry(r.Context(), h.pool, entry); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	meta, _ := json.Marshal(map[string]string{"version": entry.Version})
	_ = InsertAuditLog(r.Context(), h.pool, user.ID, user.Email, "changelog.create", "changelog_entry", entry.ID, entry.Version, meta, r.RemoteAddr)

	writeJSON(w, http.StatusCreated, entry)
}

func (h *ChangelogHandler) Update(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	var body changelogEntryBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if body.Version == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "version is required"})
		return
	}
	if body.ReleaseDate == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "release_date is required"})
		return
	}

	published := true
	if body.Published != nil {
		published = *body.Published
	}

	entry := &ChangelogEntry{
		ID:          id,
		Version:     body.Version,
		ReleaseDate: body.ReleaseDate,
		Title:       body.Title,
		Summary:     body.Summary,
		Sections:    body.Sections,
		Published:   published,
	}

	if err := UpdateChangelogEntry(r.Context(), h.pool, entry); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "entry not found"})
		return
	}

	meta, _ := json.Marshal(map[string]string{"version": entry.Version})
	_ = InsertAuditLog(r.Context(), h.pool, user.ID, user.Email, "changelog.update", "changelog_entry", id, entry.Version, meta, r.RemoteAddr)

	writeJSON(w, http.StatusOK, entry)
}

func (h *ChangelogHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}

	if err := DeleteChangelogEntry(r.Context(), h.pool, id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "entry not found"})
		return
	}

	meta, _ := json.Marshal(map[string]string{"id": id})
	_ = InsertAuditLog(r.Context(), h.pool, user.ID, user.Email, "changelog.delete", "changelog_entry", id, "", meta, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
