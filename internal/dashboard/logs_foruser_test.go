package dashboard

// logs_foruser_test.go — mcp-read-only-tools T14: LogsMetricsForUser, the
// shared operation behind the LogsMetrics REST handler and
// orbit_get_logs_metrics. Exercises both ListOwnedAppNames branches
// (superadmin/CanReadAnyApp → unrestricted, regular member → restricted to
// owned app names) against a real RingBuffer seeded with LogEntry rows,
// same pool-provisioning approach as the other *_foruser_test.go files.
//
// Skips if TEST_DATABASE_URL is not set.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func logsMetricsTestPool(t *testing.T) (*db.Pool, map[string]*DashboardUser, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}

	if _, err := pool.Exec(ctx, `DROP SCHEMA IF EXISTS zeep_system CASCADE`); err != nil {
		pool.Close()
		t.Fatalf("drop zeep_system: %v", err)
	}
	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("ProvisionZeepSystem: %v", err)
	}

	actors := map[string]*DashboardUser{}
	for _, ad := range []struct{ key, role string }{
		{"superadmin", "superadmin"},
		{"member", "member"},
	} {
		email := fmt.Sprintf("logsmetrics-%s-%d@example.com", ad.key, time.Now().UnixNano())
		u, err := CreateUser(ctx, pool, email, ad.key, "hash", ad.role)
		if err != nil {
			pool.Close()
			t.Fatalf("create user %s: %v", email, err)
		}
		actors[ad.key] = u
	}

	memberAppName := fmt.Sprintf("logsmetricsmemberapp%d", time.Now().UnixNano())
	// CreateApp already adds the owner to app_members as admin — no
	// separate AddAppMember call needed.
	if _, err := CreateApp(ctx, pool, memberAppName, actors["member"].ID, false); err != nil {
		pool.Close()
		t.Fatalf("CreateApp: %v", err)
	}

	return pool, actors, memberAppName
}

// TestLogsMetricsForUser_SuperadminSeesEveryApp covers T14's Done-when:
// LogsMetricsForUser returns the same LogMetrics shape LogsMetrics's
// existing REST handler returns, for a superadmin — the unrestricted
// (nil allowedApps) branch of ListOwnedAppNames.
func TestLogsMetricsForUser_SuperadminSeesEveryApp(t *testing.T) {
	pool, actors, memberAppName := logsMetricsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	logs := NewRingBuffer(100)
	logs.Push(LogEntry{Timestamp: time.Now(), App: memberAppName, Method: "GET", Path: "/" + memberAppName + "/widgets", Status: 200, LatencyMs: 10})
	logs.Push(LogEntry{Timestamp: time.Now(), App: "some-other-app", Method: "GET", Path: "/some-other-app/widgets", Status: 200, LatencyMs: 20})

	metrics, err := LogsMetricsForUser(ctx, pool, logs, actors["superadmin"])
	if err != nil {
		t.Fatalf("LogsMetricsForUser: %v", err)
	}
	if metrics.TotalRequests != 2 {
		t.Fatalf("expected superadmin to see both apps' requests (total=2), got %+v", metrics)
	}
	if metrics.RequestsPerApp[memberAppName] != 1 || metrics.RequestsPerApp["some-other-app"] != 1 {
		t.Fatalf("expected both apps represented in RequestsPerApp, got %+v", metrics.RequestsPerApp)
	}
}

// TestLogsMetricsForUser_MemberRestrictedToOwnApps covers T14's Done-when:
// a regular member only sees requests for apps they belong to — the
// restricted-set branch of ListOwnedAppNames.
func TestLogsMetricsForUser_MemberRestrictedToOwnApps(t *testing.T) {
	pool, actors, memberAppName := logsMetricsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	logs := NewRingBuffer(100)
	logs.Push(LogEntry{Timestamp: time.Now(), App: memberAppName, Method: "GET", Path: "/" + memberAppName + "/widgets", Status: 200, LatencyMs: 10})
	logs.Push(LogEntry{Timestamp: time.Now(), App: "some-other-app", Method: "GET", Path: "/some-other-app/widgets", Status: 200, LatencyMs: 20})

	metrics, err := LogsMetricsForUser(ctx, pool, logs, actors["member"])
	if err != nil {
		t.Fatalf("LogsMetricsForUser: %v", err)
	}
	if metrics.TotalRequests != 1 {
		t.Fatalf("expected member to see only their own app's requests (total=1), got %+v", metrics)
	}
	if _, ok := metrics.RequestsPerApp[memberAppName]; !ok {
		t.Fatalf("expected %s in RequestsPerApp, got %+v", memberAppName, metrics.RequestsPerApp)
	}
	if _, ok := metrics.RequestsPerApp["some-other-app"]; ok {
		t.Fatalf("expected some-other-app to be excluded from a member's RequestsPerApp, got %+v", metrics.RequestsPerApp)
	}
}
