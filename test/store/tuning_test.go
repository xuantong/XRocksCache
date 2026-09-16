package store_test

import (
	"testing"

	"xrockscache/internal/store"
)

const (
	testKiB uint64 = 1024
	testMiB uint64 = 1024 * testKiB
	testGiB uint64 = 1024 * testMiB
)

func TestBuildRocksDBTuningFor2C4G100G(t *testing.T) {
	tuning := store.BuildRocksDBTuning(store.ResourceProfile{
		CPUCores:      2,
		MemoryBytes:   4 * testGiB,
		DiskFreeBytes: 125 * testGiB,
	})

	if !tuning.AutoTuned {
		t.Fatal("expected auto tuned RocksDB defaults")
	}
	if tuning.Compression != "lz4" {
		t.Fatalf("Compression = %q, want lz4", tuning.Compression)
	}
	if tuning.DiskBudgetBytes != 100*testGiB {
		t.Fatalf("DiskBudgetBytes = %d, want %d", tuning.DiskBudgetBytes, 100*testGiB)
	}
	if tuning.BlockCacheSizeBytes == 0 || tuning.WriteBufferSizeBytes == 0 {
		t.Fatalf("expected non-zero memory split: %#v", tuning)
	}
	if tuning.MaxBackgroundJobs != 2 {
		t.Fatalf("MaxBackgroundJobs = %d, want 2", tuning.MaxBackgroundJobs)
	}
	if tuning.MaxSubcompactions != 1 {
		t.Fatalf("MaxSubcompactions = %d, want 1", tuning.MaxSubcompactions)
	}
	if !tuning.EnableBlobFiles || !tuning.EnableBlobGarbageCollection {
		t.Fatalf("expected blob files and blob GC enabled: %#v", tuning)
	}
	if tuning.MinBlobSizeBytes != 16*testKiB {
		t.Fatalf("MinBlobSizeBytes = %d, want %d", tuning.MinBlobSizeBytes, 16*testKiB)
	}
	if tuning.TargetFileSizeBaseBytes != 128*testMiB {
		t.Fatalf("TargetFileSizeBaseBytes = %d, want %d", tuning.TargetFileSizeBaseBytes, 128*testMiB)
	}
	if tuning.BlobFileSizeBytes != 512*testMiB {
		t.Fatalf("BlobFileSizeBytes = %d, want %d", tuning.BlobFileSizeBytes, 512*testMiB)
	}
}

func TestBuildRocksDBTuningFor4C8G(t *testing.T) {
	tuning := store.BuildRocksDBTuning(store.ResourceProfile{
		CPUCores:      4,
		MemoryBytes:   8 * testGiB,
		DiskFreeBytes: 250 * testGiB,
	})

	if tuning.MaxBackgroundJobs != 3 {
		t.Fatalf("MaxBackgroundJobs = %d, want conservative 3", tuning.MaxBackgroundJobs)
	}
	if tuning.MaxSubcompactions != 1 {
		t.Fatalf("MaxSubcompactions = %d, want conservative 1", tuning.MaxSubcompactions)
	}
	if tuning.WriteBufferSizeBytes != 256*testMiB {
		t.Fatalf("WriteBufferSizeBytes = %d, want %d", tuning.WriteBufferSizeBytes, 256*testMiB)
	}
	if tuning.HardPendingCompactionBytesLimit != 16*testGiB {
		t.Fatalf("HardPendingCompactionBytesLimit = %d, want %d", tuning.HardPendingCompactionBytesLimit, 16*testGiB)
	}
}

func TestBuildRocksDBTuningClampsSmallDisk(t *testing.T) {
	tuning := store.BuildRocksDBTuning(store.ResourceProfile{
		CPUCores:      1,
		MemoryBytes:   2 * testGiB,
		DiskFreeBytes: 5 * testGiB,
	})

	if tuning.DiskBudgetBytes != 10*testGiB {
		t.Fatalf("DiskBudgetBytes = %d, want minimum %d", tuning.DiskBudgetBytes, 10*testGiB)
	}
	if tuning.TargetFileSizeBaseBytes != 64*testMiB {
		t.Fatalf("TargetFileSizeBaseBytes = %d, want minimum %d", tuning.TargetFileSizeBaseBytes, 64*testMiB)
	}
	if tuning.BlobFileSizeBytes != 256*testMiB {
		t.Fatalf("BlobFileSizeBytes = %d, want minimum %d", tuning.BlobFileSizeBytes, 256*testMiB)
	}
}

func TestBuildRocksDBTuningUsesExplicitDiskBudget(t *testing.T) {
	tuning := store.BuildRocksDBTuning(store.ResourceProfile{
		CPUCores:        8,
		MemoryBytes:     16 * testGiB,
		DiskFreeBytes:   400 * testGiB,
		DiskBudgetBytes: 150 * testGiB,
	})

	if tuning.DiskBudgetBytes != 150*testGiB {
		t.Fatalf("DiskBudgetBytes = %d, want explicit %d", tuning.DiskBudgetBytes, 150*testGiB)
	}
	if tuning.DiskRejectWatermarkBytes != 150*testGiB*95/100 {
		t.Fatalf("DiskRejectWatermarkBytes = %d, want 95%% of budget", tuning.DiskRejectWatermarkBytes)
	}
}
