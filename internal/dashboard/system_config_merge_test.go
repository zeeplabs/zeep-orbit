package dashboard

import "testing"

func TestMergeSystemConfigPreservesAbsentFields(t *testing.T) {
	cur := SystemConfig{
		SoftDeleteEnabled: true,
		StorageConfig:     &GlobalStorageConfig{Bucket: "b1", Region: "r1"},
		MaxCSVExportRows:  5000,
	}
	// Patch toggles ONLY soft-delete off; storage + csv must survive.
	sd := false
	merged := mergeSystemConfig(cur, systemConfigPatch{SoftDeleteEnabled: &sd})
	if merged.SoftDeleteEnabled != false {
		t.Fatalf("soft delete should be updated to false")
	}
	if merged.StorageConfig == nil || merged.StorageConfig.Bucket != "b1" {
		t.Fatalf("storage config must be preserved when absent from patch, got %+v", merged.StorageConfig)
	}
	if merged.MaxCSVExportRows != 5000 {
		t.Fatalf("max csv rows must be preserved when absent, got %d", merged.MaxCSVExportRows)
	}
}

func TestMergeSystemConfigAppliesPresentFields(t *testing.T) {
	cur := SystemConfig{SoftDeleteEnabled: true, MaxCSVExportRows: 10000}
	n := 2000
	merged := mergeSystemConfig(cur, systemConfigPatch{MaxCSVExportRows: &n})
	if merged.MaxCSVExportRows != 2000 {
		t.Fatalf("max csv rows should update to 2000, got %d", merged.MaxCSVExportRows)
	}
	if merged.SoftDeleteEnabled != true {
		t.Fatalf("soft delete should be preserved, got %v", merged.SoftDeleteEnabled)
	}
}

func TestMergeSystemConfigStatementTimeout(t *testing.T) {
	cur := SystemConfig{SoftDeleteEnabled: true, StatementTimeoutMs: 30000, MaxCSVExportRows: 10000}

	// Absent from patch → preserved.
	sd := false
	merged := mergeSystemConfig(cur, systemConfigPatch{SoftDeleteEnabled: &sd})
	if merged.StatementTimeoutMs != 30000 {
		t.Fatalf("statement timeout must be preserved when absent, got %d", merged.StatementTimeoutMs)
	}

	// Present zero → applied (0 disables the timeout; it is a real value, not "absent").
	zero := 0
	merged = mergeSystemConfig(cur, systemConfigPatch{StatementTimeoutMs: &zero})
	if merged.StatementTimeoutMs != 0 {
		t.Fatalf("statement timeout 0 must be applied (disabled), got %d", merged.StatementTimeoutMs)
	}

	// Present positive → applied.
	n := 5000
	merged = mergeSystemConfig(cur, systemConfigPatch{StatementTimeoutMs: &n})
	if merged.StatementTimeoutMs != 5000 {
		t.Fatalf("statement timeout should update to 5000, got %d", merged.StatementTimeoutMs)
	}
}

func TestMergeSystemConfigRequireRLSDefault(t *testing.T) {
	cur := SystemConfig{RequireRLSDefault: true, StatementTimeoutMs: 30000}

	// Absent from patch → preserved.
	n := 5000
	merged := mergeSystemConfig(cur, systemConfigPatch{StatementTimeoutMs: &n})
	if !merged.RequireRLSDefault {
		t.Fatalf("require_rls_default must be preserved when absent, got %v", merged.RequireRLSDefault)
	}

	// Present false → applied (turning it off is a real change, not "absent").
	off := false
	merged = mergeSystemConfig(cur, systemConfigPatch{RequireRLSDefault: &off})
	if merged.RequireRLSDefault {
		t.Fatalf("require_rls_default false must be applied, got %v", merged.RequireRLSDefault)
	}
}

func TestMergeSystemConfigRetentionDays(t *testing.T) {
	cur := SystemConfig{RetentionDays: 30, SoftDeleteEnabled: true}

	// Absent from patch → preserved.
	sd := false
	merged := mergeSystemConfig(cur, systemConfigPatch{SoftDeleteEnabled: &sd})
	if merged.RetentionDays != 30 {
		t.Fatalf("retention_days must be preserved when absent, got %d", merged.RetentionDays)
	}

	// Present zero → applied (0 disables purge; it is a real value, not "absent").
	zero := 0
	merged = mergeSystemConfig(cur, systemConfigPatch{RetentionDays: &zero})
	if merged.RetentionDays != 0 {
		t.Fatalf("retention_days 0 must be applied (disabled), got %d", merged.RetentionDays)
	}
}
