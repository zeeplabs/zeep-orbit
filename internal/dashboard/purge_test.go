package dashboard

import "testing"

func TestPurgeExpiredSoftDeletesOffByDefault(t *testing.T) {
	n, err := PurgeExpiredSoftDeletes(nil, nil, nil, 0)
	if err != nil || n != 0 {
		t.Fatalf("expected no-op when retentionDays<=0, got n=%d err=%v", n, err)
	}
}

func TestPurgeExpiredSoftDeletesNegativeIsOff(t *testing.T) {
	n, err := PurgeExpiredSoftDeletes(nil, nil, nil, -5)
	if err != nil || n != 0 {
		t.Fatalf("expected no-op when retentionDays<0, got n=%d err=%v", n, err)
	}
}
