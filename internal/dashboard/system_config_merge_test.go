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
