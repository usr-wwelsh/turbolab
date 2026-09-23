package cmd

import (
	"testing"

	"github.com/usr-wwelsh/turbolab/internal/monitor"
)

func TestRAMFields_ReportsUsedPercentOfActualTotal(t *testing.T) {
	gb := uint64(1 << 30)
	got := ramFields(&monitor.Stats{TotalRAM: 64 * gb, AvailableRAM: 48 * gb, UsedRAM: 16 * gb})
	if got["ram_used_percent"] != 25.0 {
		t.Errorf("ram_used_percent = %v, want 25", got["ram_used_percent"])
	}
	if got["ram_available_gb"] != 48.0 {
		t.Errorf("ram_available_gb = %v, want 48", got["ram_available_gb"])
	}
}

func TestRAMFields_NilStatsReportsZero(t *testing.T) {
	got := ramFields(nil)
	if got["ram_used_percent"] != 0.0 || got["ram_available_gb"] != 0.0 {
		t.Errorf("got %v, want zeros", got)
	}
}
