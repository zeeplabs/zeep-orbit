package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/auth"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
	"github.com/zeeplabs/zeep-orbit/internal/storage"
)

func (h *Handler) newStorageClient(ctx context.Context, app *registry.App) (*storage.Client, error) {
	if app.StorageConfig == nil {
		return nil, fmt.Errorf("storage not configured for this app")
	}
	sc := storage.StorageConfig{
		Bucket:          app.StorageConfig.Bucket,
		Region:          app.StorageConfig.Region,
		Endpoint:        app.StorageConfig.Endpoint,
		AccessKeyID:     app.StorageConfig.AccessKeyID,
		SecretAccessKey: app.StorageConfig.SecretAccessKey,
	}
	return storage.NewClient(ctx, sc)
}

type fileResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	MimeType  string `json:"mime_type"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
}

func randomID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func filesTable(schema string) string {
	return fmt.Sprintf("%q.\"_files\"", schema)
}

func (h *Handler) HandleFileUpload(w http.ResponseWriter, r *http.Request) {
	app, ok := AppFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusInternalServerError, "app not in context")
		return
	}

	if app.StorageConfig == nil {
		writeError(w, http.StatusBadRequest, "storage not configured for this app")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field is required")
		return
	}
	defer file.Close()

	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	ext := strings.ToLower(path.Ext(header.Filename))
	fileID := randomID()
	schemaName := "app_" + app.Config.Name
	key := fmt.Sprintf("%s/%s%s", schemaName, fileID, ext)

	s3Client, err := 	h.newStorageClient(r.Context(), app)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage client error")
		return
	}

	if err := s3Client.Upload(r.Context(), key, file, mimeType); err != nil {
		writeError(w, http.StatusInternalServerError, "upload failed")
		return
	}

	var ownerID string
	if user, hasUser := auth.UserFromContext(r.Context()); hasUser {
		ownerID = user.ID
	}

	var rowID string
	var createdAt time.Time
	q := fmt.Sprintf(`INSERT INTO %s (name, size, mime_type, key, owner_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`, filesTable(schemaName))
	err = h.pool.QueryRow(r.Context(), q,
		header.Filename, header.Size, mimeType, key, ownerID,
	).Scan(&rowID, &createdAt)
	if err != nil {
		s3Client.Delete(r.Context(), key)
		writeError(w, http.StatusInternalServerError, "failed to save file metadata")
		return
	}

	writeJSON(w, http.StatusCreated, fileResponse{
		ID:        rowID,
		Name:      header.Filename,
		Size:      header.Size,
		MimeType:  mimeType,
		URL:       fmt.Sprintf("/%s/files/%s/download", app.Config.Name, rowID),
		CreatedAt: createdAt.Format(time.RFC3339),
	})
}

func (h *Handler) HandleFileList(w http.ResponseWriter, r *http.Request) {
	app, ok := AppFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusInternalServerError, "app not in context")
		return
	}

	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	schemaName := "app_" + app.Config.Name
	q := fmt.Sprintf(`SELECT id, name, size, mime_type, created_at
		FROM %s ORDER BY created_at DESC LIMIT $1 OFFSET $2`, filesTable(schemaName))

	rows, err := h.pool.Query(r.Context(), q, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list files")
		return
	}
	defer rows.Close()

	var files []fileResponse
	for rows.Next() {
		var f fileResponse
		var createdAt time.Time
		if err := rows.Scan(&f.ID, &f.Name, &f.Size, &f.MimeType, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan file")
			return
		}
		f.CreatedAt = createdAt.Format(time.RFC3339)
		f.URL = fmt.Sprintf("/%s/files/%s/download", app.Config.Name, f.ID)
		files = append(files, f)
	}
	if files == nil {
		files = []fileResponse{}
	}

	writeJSON(w, http.StatusOK, files)
}

func (h *Handler) HandleFileGet(w http.ResponseWriter, r *http.Request) {
	app, ok := AppFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusInternalServerError, "app not in context")
		return
	}

	fileID := chi.URLParam(r, "id")
	schemaName := "app_" + app.Config.Name
	q := fmt.Sprintf(`SELECT id, name, size, mime_type, created_at
		FROM %s WHERE id = $1`, filesTable(schemaName))

	var f fileResponse
	var createdAt time.Time
	err := h.pool.QueryRow(r.Context(), q, fileID).Scan(&f.ID, &f.Name, &f.Size, &f.MimeType, &createdAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get file")
		return
	}
	f.CreatedAt = createdAt.Format(time.RFC3339)
	f.URL = fmt.Sprintf("/%s/files/%s/download", app.Config.Name, f.ID)

	writeJSON(w, http.StatusOK, f)
}

func (h *Handler) HandleFileDownload(w http.ResponseWriter, r *http.Request) {
	app, ok := AppFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusInternalServerError, "app not in context")
		return
	}

	if app.StorageConfig == nil {
		writeError(w, http.StatusBadRequest, "storage not configured")
		return
	}

	fileID := chi.URLParam(r, "id")
	schemaName := "app_" + app.Config.Name

	var fileKey string
	err := h.pool.QueryRow(r.Context(),
		fmt.Sprintf(`SELECT key FROM %s WHERE id = $1`, filesTable(schemaName)),
		fileID,
	).Scan(&fileKey)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get file")
		return
	}

	s3Client, err := 	h.newStorageClient(r.Context(), app)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage client error")
		return
	}

	ttl := 1 * time.Hour
	if t := r.URL.Query().Get("ttl"); t != "" {
		if d, err := time.ParseDuration(t + "s"); err == nil && d > 0 && d <= 24*time.Hour {
			ttl = d
		}
	}

	signedURL, err := s3Client.SignedURL(r.Context(), fileKey, ttl)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate download url")
		return
	}

	http.Redirect(w, r, signedURL, http.StatusFound)
}

func (h *Handler) HandleFileDelete(w http.ResponseWriter, r *http.Request) {
	app, ok := AppFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusInternalServerError, "app not in context")
		return
	}

	if app.StorageConfig == nil {
		writeError(w, http.StatusBadRequest, "storage not configured")
		return
	}

	fileID := chi.URLParam(r, "id")
	schemaName := "app_" + app.Config.Name

	var key string
	err := h.pool.QueryRow(r.Context(),
		fmt.Sprintf(`SELECT key FROM %s WHERE id = $1`, filesTable(schemaName)),
		fileID,
	).Scan(&key)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get file")
		return
	}

	s3Client, err := 	h.newStorageClient(r.Context(), app)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage client error")
		return
	}

	if err := s3Client.Delete(r.Context(), key); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete file")
		return
	}

	_, err = h.pool.Exec(r.Context(),
		fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, filesTable(schemaName)),
		fileID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete file metadata")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) HandleFileSignedURL(w http.ResponseWriter, r *http.Request) {
	app, ok := AppFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusInternalServerError, "app not in context")
		return
	}

	if app.StorageConfig == nil {
		writeError(w, http.StatusBadRequest, "storage not configured")
		return
	}

	fileID := chi.URLParam(r, "id")
	schemaName := "app_" + app.Config.Name

	var key string
	err := h.pool.QueryRow(r.Context(),
		fmt.Sprintf(`SELECT key FROM %s WHERE id = $1`, filesTable(schemaName)),
		fileID,
	).Scan(&key)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get file")
		return
	}

	ttl := 1 * time.Hour
	if t := r.URL.Query().Get("ttl"); t != "" {
		if d, err := time.ParseDuration(t + "s"); err == nil && d > 0 && d <= 24*time.Hour {
			ttl = d
		}
	}

	s3Client, err := 	h.newStorageClient(r.Context(), app)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage client error")
		return
	}

	signedURL, err := s3Client.SignedURL(r.Context(), key, ttl)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate signed url")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"url": signedURL})
}
