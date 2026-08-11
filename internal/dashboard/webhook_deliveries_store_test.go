package dashboard

import (
	"context"
	"encoding/json"
	"testing"
)

func TestInsertDelivery_And_ListDeliveries_HappyPath(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()

	wh, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "hook", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	payload, _ := json.Marshal(map[string]any{"eventType": "user.created", "id": "abc"})
	if err := InsertDelivery(ctx, pool, DeliveryEntry{
		WebhookID:      wh.ID,
		HTTPStatus:     200,
		Outcome:        "captured",
		EventTypeValue: "user.created",
		RawPayload:     payload,
	}); err != nil {
		t.Fatalf("InsertDelivery: %v", err)
	}

	list, err := ListDeliveries(ctx, pool, wh.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(list))
	}
	if list[0].Outcome != "captured" {
		t.Fatalf("expected outcome=captured, got %q", list[0].Outcome)
	}
	if list[0].EventTypeValue == nil || *list[0].EventTypeValue != "user.created" {
		t.Fatalf("expected event_type_value='user.created', got %v", list[0].EventTypeValue)
	}
	if list[0].RawPayload["id"] != "abc" {
		t.Fatalf("expected raw_payload.id == 'abc', got %v", list[0].RawPayload)
	}
}

func TestListDeliveries_NewestFirst(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()

	wh, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "hook", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	if err := InsertDelivery(ctx, pool, DeliveryEntry{WebhookID: wh.ID, HTTPStatus: 200, Outcome: "captured"}); err != nil {
		t.Fatalf("InsertDelivery (1st): %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE zeep_system.webhook_deliveries SET received_at = now() - interval '1 hour' WHERE webhook_id = $1`, wh.ID); err != nil {
		t.Fatalf("backdate first delivery: %v", err)
	}
	if err := InsertDelivery(ctx, pool, DeliveryEntry{WebhookID: wh.ID, HTTPStatus: 401, Outcome: "invalid_token"}); err != nil {
		t.Fatalf("InsertDelivery (2nd): %v", err)
	}

	list, err := ListDeliveries(ctx, pool, wh.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 deliveries, got %d", len(list))
	}
	if list[0].Outcome != "invalid_token" || list[1].Outcome != "captured" {
		t.Fatalf("expected newest-first order [invalid_token, captured], got [%s, %s]", list[0].Outcome, list[1].Outcome)
	}
}

func TestHasProcessedEventID(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()

	wh, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "hook", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	seen, err := HasProcessedEventID(ctx, pool, wh.ID, "evt-1")
	if err != nil {
		t.Fatalf("HasProcessedEventID (before insert): %v", err)
	}
	if seen {
		t.Fatal("expected not-yet-seen event id to report false")
	}

	if err := InsertDelivery(ctx, pool, DeliveryEntry{
		WebhookID: wh.ID, HTTPStatus: 200, Outcome: "inserted", EventID: "evt-1",
	}); err != nil {
		t.Fatalf("InsertDelivery: %v", err)
	}

	seen, err = HasProcessedEventID(ctx, pool, wh.ID, "evt-1")
	if err != nil {
		t.Fatalf("HasProcessedEventID (after insert): %v", err)
	}
	if !seen {
		t.Fatal("expected previously-recorded event id to report true")
	}
}

func TestPurgeExpiredDeliveries_RemovesOnlyOldRows(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()

	whOld, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "old-hook", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook (old): %v", err)
	}
	whRecent, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "recent-hook", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook (recent): %v", err)
	}

	if err := InsertDelivery(ctx, pool, DeliveryEntry{WebhookID: whOld.ID, HTTPStatus: 200, Outcome: "captured"}); err != nil {
		t.Fatalf("InsertDelivery (old): %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE zeep_system.webhook_deliveries SET received_at = now() - interval '40 days' WHERE webhook_id = $1`, whOld.ID); err != nil {
		t.Fatalf("backdate old delivery: %v", err)
	}
	if err := InsertDelivery(ctx, pool, DeliveryEntry{WebhookID: whRecent.ID, HTTPStatus: 200, Outcome: "captured"}); err != nil {
		t.Fatalf("InsertDelivery (recent): %v", err)
	}

	n, err := PurgeExpiredDeliveries(ctx, pool, 30)
	if err != nil {
		t.Fatalf("PurgeExpiredDeliveries: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 row purged, got %d", n)
	}

	oldList, err := ListDeliveries(ctx, pool, whOld.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries (old): %v", err)
	}
	if len(oldList) != 0 {
		t.Fatalf("expected 0 remaining deliveries for the old webhook, got %d", len(oldList))
	}
	recentList, err := ListDeliveries(ctx, pool, whRecent.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries (recent): %v", err)
	}
	if len(recentList) != 1 {
		t.Fatalf("expected the recent webhook's delivery to remain untouched, got %d", len(recentList))
	}
}

func TestPurgeExpiredDeliveries_NoOpWhenNothingExpired(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()

	wh, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "hook", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if err := InsertDelivery(ctx, pool, DeliveryEntry{WebhookID: wh.ID, HTTPStatus: 200, Outcome: "captured"}); err != nil {
		t.Fatalf("InsertDelivery: %v", err)
	}

	n, err := PurgeExpiredDeliveries(ctx, pool, 30)
	if err != nil {
		t.Fatalf("PurgeExpiredDeliveries: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 rows purged when nothing is expired, got %d", n)
	}

	list, err := ListDeliveries(ctx, pool, wh.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected the fresh delivery to remain, got %d", len(list))
	}
}
