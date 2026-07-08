// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"testing"
	"time"
)

func TestBackupName(t *testing.T) {
	ts := time.Date(2026, 7, 8, 19, 3, 45, 0, time.UTC)
	tests := []struct {
		name         string
		version      int
		versionStamp bool
		want         string
	}{
		{"plain", 1, false, "ec.db.20260708T190345Z"},
		{"stamped", 1, true, "ec.db.20260708T190345Z-1"},
		{"stamped multi-digit", 12, true, "ec.db.20260708T190345Z-12"},
		{"version ignored when not stamping", 12, false, "ec.db.20260708T190345Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := backupName(ts, tt.version, tt.versionStamp); got != tt.want {
				t.Errorf("backupName(%v, %d, %v) = %q, want %q", ts, tt.version, tt.versionStamp, got, tt.want)
			}
		})
	}
}

// TestBackupNameUsesUTC confirms a non-UTC input is normalized to UTC in the name.
func TestBackupNameUsesUTC(t *testing.T) {
	// 14:03:45 in a UTC-5 zone is 19:03:45 UTC.
	zone := time.FixedZone("UTC-5", -5*60*60)
	ts := time.Date(2026, 7, 8, 14, 3, 45, 0, zone)
	if got, want := backupName(ts, 1, false), "ec.db.20260708T190345Z"; got != want {
		t.Errorf("backupName = %q, want %q (UTC-normalized)", got, want)
	}
}
