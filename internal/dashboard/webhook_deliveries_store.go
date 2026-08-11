package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// DeliveryEntry is the append-only write payload for one recorded call to a
// webhook — written regardless of outcome (spec P1 AC6). Optional fields use
// the empty string / nil raw payload to mean "not applicable for this
// outcome" (e.g. EventTypeValue is empty for a malformed-body delivery that
// never got far enough to resolve one).
type DeliveryEntry struct {
	WebhookID      string
	HTTPStatus     int
	Outcome        string // captured|inserted|updated|deleted|unmapped|duplicate_skipped|row_not_found|invalid_token|malformed|write_error|verification_challenge|ambiguous_match
	EventTypeValue string
	EventID        string
	RawPayload     json.RawMessage
	TargetRowID    string
	ErrorDetail    string // dashboard-visible only, never returned to the external caller (AGENTS.md §4)
}

// DeliveryRow is a row from zeep_system.webhook_deliveries.
type DeliveryRow struct {
	ID             string
	WebhookID      string
	ReceivedAt     time.Time
	HTTPStatus     int
	Outcome        string
	EventTypeValue *string
	EventID        *string
	RawPayload     map[string]any
	TargetRowID    *string
	ErrorDetail    *string
}

// InsertDelivery records one call to a webhook. Never returns an error that
// blocks the caller from still responding to the external provider — the
// handler decides what to do if logging itself fails, but this function's
// job is just the write.
func InsertDelivery(ctx context.Context, pool *db.Pool, entry DeliveryEntry) error {
	payload := entry.RawPayload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}

	nullableStr := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}

	_, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.webhook_deliveries
		 (webhook_id, http_status, outcome, event_type_value, event_id, raw_payload, target_row_id, error_detail)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		entry.WebhookID, entry.HTTPStatus, entry.Outcome,
		nullableStr(entry.EventTypeValue), nullableStr(entry.EventID), payload,
		nullableStr(entry.TargetRowID), nullableStr(entry.ErrorDetail),
	)
	if err != nil {
		return fmt.Errorf("dashboard: insert webhook delivery: %w", err)
	}
	return nil
}

func scanDeliveryRow(scan func(dest ...any) error) (DeliveryRow, error) {
	var d DeliveryRow
	var rawPayload []byte
	if err := scan(&d.ID, &d.WebhookID, &d.ReceivedAt, &d.HTTPStatus, &d.Outcome,
		&d.EventTypeValue, &d.EventID, &rawPayload, &d.TargetRowID, &d.ErrorDetail); err != nil {
		return DeliveryRow{}, err
	}
	if len(rawPayload) > 0 {
		if err := json.Unmarshal(rawPayload, &d.RawPayload); err != nil {
			return DeliveryRow{}, fmt.Errorf("dashboard: unmarshal delivery raw_payload: %w", err)
		}
	}
	return d, nil
}

// ListDeliveries returns a webhook's recorded deliveries, newest first
// (spec P2 dashboard-delivery-log AC1).
func ListDeliveries(ctx context.Context, pool *db.Pool, webhookID string, limit, offset int) ([]DeliveryRow, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, webhook_id, received_at, http_status, outcome, event_type_value, event_id, raw_payload, target_row_id, error_detail
		 FROM zeep_system.webhook_deliveries
		 WHERE webhook_id = $1
		 ORDER BY received_at DESC
		 LIMIT $2 OFFSET $3`,
		webhookID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("dashboard: list webhook deliveries: %w", err)
	}
	defer rows.Close()

	result := make([]DeliveryRow, 0)
	for rows.Next() {
		d, err := scanDeliveryRow(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("dashboard: scan webhook delivery row: %w", err)
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

// processedOutcomes are the delivery outcomes that count as a true duplicate
// on repeat. write_error and row_not_found are excluded: the event was not
// durably processed, so a provider retry with the same event_id must be
// allowed to try again.
var processedOutcomes = []string{"inserted", "updated", "deleted", "unmapped", "duplicate_skipped"}

// HasProcessedEventID reports whether a delivery already exists for
// (webhookID, eventID) with an outcome that represents genuine processing.
func HasProcessedEventID(ctx context.Context, pool *db.Pool, webhookID, eventID string) (bool, error) {
	if eventID == "" {
		return false, nil
	}
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM zeep_system.webhook_deliveries WHERE webhook_id = $1 AND event_id = $2 AND outcome = ANY($3))`,
		webhookID, eventID, processedOutcomes,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("dashboard: check processed event id for webhook %s: %w", webhookID, err)
	}
	return exists, nil
}

// PurgeExpiredDeliveries hard-deletes delivery rows older than
// retentionDays, across every webhook regardless of its active/inactive/
// (soft-)deleted state (spec Edge Cases: "the purge runs regardless of a
// webhook's active/inactive/deleted state" — there is no per-webhook filter
// here on purpose).
func PurgeExpiredDeliveries(ctx context.Context, pool *db.Pool, retentionDays int) (int, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	tag, err := pool.Exec(ctx,
		`DELETE FROM zeep_system.webhook_deliveries WHERE received_at < now() - $1::interval`,
		fmt.Sprintf("%d days", retentionDays),
	)
	if err != nil {
		return 0, fmt.Errorf("dashboard: purge expired webhook deliveries: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
