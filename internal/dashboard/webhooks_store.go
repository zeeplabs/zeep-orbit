package dashboard

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// ErrNoMappings is returned by ActivateWebhook when the webhook has zero
// saved event mappings — spec edge case: activation must be rejected, never
// a silent no-op (inbound-webhooks spec, Edge Cases).
var ErrNoMappings = errors.New("dashboard: webhook has no saved mappings")

// ErrWebhookNotFound is returned when a webhook id does not resolve to a
// live (non-soft-deleted) row, scoped to appID when appID is non-empty.
var ErrWebhookNotFound = errors.New("dashboard: webhook not found")

// WebhookRow is a row from zeep_system.webhook_subscriptions.
type WebhookRow struct {
	ID             string
	AppID          string
	Name           string
	Method         string
	TokenHash      string
	EventTypePath  string
	EventIDPath    *string
	Status         string
	CapturedSample map[string]any
	DeletedAt      *time.Time
	CreatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CreateWebhookInput is the create-webhook request payload.
type CreateWebhookInput struct {
	AppID         string
	Name          string
	Method        string
	EventTypePath string
	EventIDPath   string // optional; empty means no dedup path configured
	CreatedBy     string
}

// generateWebhookToken mirrors handler.go's generateToken (32-byte
// crypto/rand, hex-encoded) — same entropy budget, but hashed before
// persisting instead of stored verbatim (see design.md Tech Decisions: a
// 256-bit random secret doesn't need a slow password hash).
func generateWebhookToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hashWebhookToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateWebhook generates a random token, persists only its SHA-256 hash,
// and returns the plaintext token exactly once (the caller/handler is the
// only place it is ever exposed again).
func CreateWebhook(ctx context.Context, pool *db.Pool, input CreateWebhookInput) (WebhookRow, string, error) {
	switch input.Method {
	case "GET", "POST", "PUT", "PATCH":
	default:
		return WebhookRow{}, "", fmt.Errorf("dashboard: invalid webhook method %q", input.Method)
	}

	token, err := generateWebhookToken()
	if err != nil {
		return WebhookRow{}, "", fmt.Errorf("dashboard: generate webhook token: %w", err)
	}
	tokenHash := hashWebhookToken(token)

	var eventIDPath *string
	if input.EventIDPath != "" {
		eventIDPath = &input.EventIDPath
	}

	var row WebhookRow
	err = pool.QueryRow(ctx,
		`INSERT INTO zeep_system.webhook_subscriptions
		 (app_id, name, method, token_hash, event_type_path, event_id_path, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, app_id, name, method, token_hash, event_type_path, event_id_path,
		           status, created_by, created_at, updated_at`,
		input.AppID, input.Name, input.Method, tokenHash, input.EventTypePath, eventIDPath, input.CreatedBy,
	).Scan(&row.ID, &row.AppID, &row.Name, &row.Method, &row.TokenHash, &row.EventTypePath, &row.EventIDPath,
		&row.Status, &row.CreatedBy, &row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		return WebhookRow{}, "", fmt.Errorf("dashboard: create webhook: %w", err)
	}
	return row, token, nil
}

func scanWebhookRow(row pgx.Row) (WebhookRow, error) {
	var w WebhookRow
	var capturedJSON []byte
	err := row.Scan(&w.ID, &w.AppID, &w.Name, &w.Method, &w.TokenHash, &w.EventTypePath, &w.EventIDPath,
		&w.Status, &capturedJSON, &w.DeletedAt, &w.CreatedBy, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return WebhookRow{}, err
	}
	if len(capturedJSON) > 0 {
		if err := json.Unmarshal(capturedJSON, &w.CapturedSample); err != nil {
			return WebhookRow{}, fmt.Errorf("dashboard: unmarshal captured_sample: %w", err)
		}
	}
	return w, nil
}

const webhookRowColumns = `id, app_id, name, method, token_hash, event_type_path, event_id_path,
	           status, captured_sample, deleted_at, created_by, created_at, updated_at`

// GetWebhookByID resolves a single non-soft-deleted webhook by id. When
// appID is non-empty, the lookup is additionally scoped to that app (the
// dashboard's usage: appID comes from the authenticated URL path). When
// appID is empty, the lookup is unscoped by app — the public delivery
// handler (T6) only has {webhookId} in the URL, not the owning app, so it
// resolves the webhook first and learns its app from the row.
//
// Filtering deleted_at IS NULL here (rather than in a separate function) is
// what makes "call to a deleted webhook's URL → 404" (design.md Error
// Handling Strategy) fall out naturally: a soft-deleted webhook simply
// doesn't resolve anymore, for either caller.
func GetWebhookByID(ctx context.Context, pool *db.Pool, appID, webhookID string) (WebhookRow, error) {
	var row pgx.Row
	if appID != "" {
		row = pool.QueryRow(ctx,
			`SELECT `+webhookRowColumns+` FROM zeep_system.webhook_subscriptions
			 WHERE id = $1 AND app_id = $2 AND deleted_at IS NULL`,
			webhookID, appID,
		)
	} else {
		row = pool.QueryRow(ctx,
			`SELECT `+webhookRowColumns+` FROM zeep_system.webhook_subscriptions
			 WHERE id = $1 AND deleted_at IS NULL`,
			webhookID,
		)
	}
	w, err := scanWebhookRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return WebhookRow{}, ErrWebhookNotFound
		}
		return WebhookRow{}, fmt.Errorf("dashboard: get webhook %s: %w", webhookID, err)
	}
	return w, nil
}

// ListWebhooks returns every non-soft-deleted webhook for an app, newest first.
func ListWebhooks(ctx context.Context, pool *db.Pool, appID string) ([]WebhookRow, error) {
	rows, err := pool.Query(ctx,
		`SELECT `+webhookRowColumns+` FROM zeep_system.webhook_subscriptions
		 WHERE app_id = $1 AND deleted_at IS NULL
		 ORDER BY created_at DESC`,
		appID,
	)
	if err != nil {
		return nil, fmt.Errorf("dashboard: list webhooks: %w", err)
	}
	defer rows.Close()

	result := make([]WebhookRow, 0)
	for rows.Next() {
		w, err := scanWebhookRow(rows)
		if err != nil {
			return nil, fmt.Errorf("dashboard: scan webhook row: %w", err)
		}
		result = append(result, w)
	}
	return result, rows.Err()
}

// StoreCapturedSample overwrites the webhook's captured_sample with the
// latest received payload (spec P1 AC3: a second capture-mode call
// overwrites, it does not accumulate).
func StoreCapturedSample(ctx context.Context, pool *db.Pool, webhookID string, payload []byte) error {
	tag, err := pool.Exec(ctx,
		`UPDATE zeep_system.webhook_subscriptions
		 SET captured_sample = $1, updated_at = now()
		 WHERE id = $2 AND deleted_at IS NULL`,
		payload, webhookID,
	)
	if err != nil {
		return fmt.Errorf("dashboard: store captured sample for webhook %s: %w", webhookID, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrWebhookNotFound
	}
	return nil
}

// ActivateWebhook flips a webhook from capture to active mode. Rejects with
// ErrNoMappings when zero mappings are saved (spec Edge Cases: "activate
// with zero saved mappings" must be a rejected validation error, not a
// silent no-op) — checked and applied in one transaction so a concurrent
// mapping delete can never race the activation into an inconsistent state.
func ActivateWebhook(ctx context.Context, pool *db.Pool, webhookID string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("dashboard: activate webhook begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var mappingCount int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM zeep_system.webhook_event_mappings WHERE webhook_id = $1`,
		webhookID,
	).Scan(&mappingCount); err != nil {
		return fmt.Errorf("dashboard: activate webhook count mappings: %w", err)
	}
	if mappingCount == 0 {
		return ErrNoMappings
	}

	tag, err := tx.Exec(ctx,
		`UPDATE zeep_system.webhook_subscriptions SET status = 'active', updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL`,
		webhookID,
	)
	if err != nil {
		return fmt.Errorf("dashboard: activate webhook %s: %w", webhookID, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrWebhookNotFound
	}

	return tx.Commit(ctx)
}

// RotateToken generates a new token, persists only its hash (immediately
// invalidating the old one), and returns the new plaintext once (spec P3
// AC1: mappings are untouched by rotation).
func RotateToken(ctx context.Context, pool *db.Pool, webhookID string) (string, error) {
	token, err := generateWebhookToken()
	if err != nil {
		return "", fmt.Errorf("dashboard: generate webhook token: %w", err)
	}
	tokenHash := hashWebhookToken(token)

	tag, err := pool.Exec(ctx,
		`UPDATE zeep_system.webhook_subscriptions SET token_hash = $1, updated_at = now()
		 WHERE id = $2 AND deleted_at IS NULL`,
		tokenHash, webhookID,
	)
	if err != nil {
		return "", fmt.Errorf("dashboard: rotate token for webhook %s: %w", webhookID, err)
	}
	if tag.RowsAffected() == 0 {
		return "", ErrWebhookNotFound
	}
	return token, nil
}

// SoftDeleteWebhook sets deleted_at — never a hard DELETE, since
// webhook_deliveries has no cascading FK and must survive until its own
// 30-day retention purge (spec P3 AC2).
func SoftDeleteWebhook(ctx context.Context, pool *db.Pool, webhookID string) error {
	tag, err := pool.Exec(ctx,
		`UPDATE zeep_system.webhook_subscriptions SET deleted_at = now(), updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL`,
		webhookID,
	)
	if err != nil {
		return fmt.Errorf("dashboard: soft delete webhook %s: %w", webhookID, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrWebhookNotFound
	}
	return nil
}
