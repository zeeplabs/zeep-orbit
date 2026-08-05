package dashboard

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/deploy/render"
)

// fakeDeployLister lets tests control ListDeploys per serviceID without
// hitting the real Render API.
type fakeDeployLister struct {
	byService map[string][]render.Deploy
	errFor    map[string]error
}

func (f *fakeDeployLister) ListDeploys(_ context.Context, serviceID string, _ int, _ []string) ([]render.Deploy, error) {
	if err, ok := f.errFor[serviceID]; ok {
		return nil, err
	}
	return f.byService[serviceID], nil
}

func TestAggregateRecentDeploys_OrdersAcrossApps(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	lister := &fakeDeployLister{
		byService: map[string][]render.Deploy{
			"svc-old": {{ID: "d1", Status: "live", CreatedAt: now.Add(-48 * time.Hour)}},
			"svc-new": {{ID: "d2", Status: "live", CreatedAt: now.Add(-1 * time.Hour)}},
		},
	}
	apps := []DeployedApp{
		{ID: "app-old", Name: "old-app", DeployServiceID: "svc-old"},
		{ID: "app-new", Name: "new-app", DeployServiceID: "svc-new"},
	}

	items := aggregateRecentDeploys(context.Background(), lister, apps, 10)

	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].AppName != "new-app" {
		t.Fatalf("expected newest deploy first (new-app), got %q", items[0].AppName)
	}
	if items[1].AppName != "old-app" {
		t.Fatalf("expected oldest deploy last (old-app), got %q", items[1].AppName)
	}
}

func TestAggregateRecentDeploys_PartialFailureIsSkipped(t *testing.T) {
	now := time.Now()
	lister := &fakeDeployLister{
		byService: map[string][]render.Deploy{
			"svc-ok": {{ID: "d1", Status: "live", CreatedAt: now}},
		},
		errFor: map[string]error{
			"svc-broken": fmt.Errorf("render: list deploys failed (status 404)"),
		},
	}
	apps := []DeployedApp{
		{ID: "app-ok", Name: "ok-app", DeployServiceID: "svc-ok"},
		{ID: "app-broken", Name: "broken-app", DeployServiceID: "svc-broken"},
	}

	items := aggregateRecentDeploys(context.Background(), lister, apps, 10)

	if len(items) != 1 {
		t.Fatalf("expected 1 item (broken app skipped), got %d", len(items))
	}
	if items[0].AppName != "ok-app" {
		t.Fatalf("expected ok-app to survive, got %q", items[0].AppName)
	}
}

func TestAggregateRecentDeploys_StatusMapping(t *testing.T) {
	now := time.Now()
	lister := &fakeDeployLister{
		byService: map[string][]render.Deploy{
			"svc-1": {
				{ID: "d1", Status: "live", CreatedAt: now},
				{ID: "d2", Status: "build_failed", CreatedAt: now.Add(-time.Minute)},
				{ID: "d3", Status: "canceled", CreatedAt: now.Add(-2 * time.Minute)},
			},
		},
	}
	apps := []DeployedApp{{ID: "app-1", Name: "app-1", DeployServiceID: "svc-1"}}

	items := aggregateRecentDeploys(context.Background(), lister, apps, 10)

	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0].Status != "Live" {
		t.Fatalf("expected live -> Live, got %q", items[0].Status)
	}
	if items[1].Status != "Failed" {
		t.Fatalf("expected build_failed -> Failed, got %q", items[1].Status)
	}
	if items[2].Status != "Failed" {
		t.Fatalf("expected canceled -> Failed, got %q", items[2].Status)
	}
}

func TestAggregateRecentDeploys_TotalLimit(t *testing.T) {
	now := time.Now()
	lister := &fakeDeployLister{
		byService: map[string][]render.Deploy{
			"svc-1": {
				{ID: "d1", Status: "live", CreatedAt: now},
				{ID: "d2", Status: "live", CreatedAt: now.Add(-time.Minute)},
				{ID: "d3", Status: "live", CreatedAt: now.Add(-2 * time.Minute)},
			},
		},
	}
	apps := []DeployedApp{{ID: "app-1", Name: "app-1", DeployServiceID: "svc-1"}}

	items := aggregateRecentDeploys(context.Background(), lister, apps, 2)

	if len(items) != 2 {
		t.Fatalf("expected total limit of 2, got %d", len(items))
	}
}

func TestAggregateRecentDeploys_NoApps(t *testing.T) {
	lister := &fakeDeployLister{byService: map[string][]render.Deploy{}}

	items := aggregateRecentDeploys(context.Background(), lister, []DeployedApp{}, 10)

	if len(items) != 0 {
		t.Fatalf("expected 0 items for no apps, got %d", len(items))
	}
}
