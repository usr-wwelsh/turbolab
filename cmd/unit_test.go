package cmd

import (
	"strings"
	"testing"
)

func sectionOf(unit, key string) string {
	section := ""
	for _, line := range strings.Split(unit, "\n") {
		if strings.HasPrefix(line, "[") {
			section = strings.Trim(line, "[]")
		}
		if strings.HasPrefix(line, key+"=") {
			return section
		}
	}
	return ""
}

func TestSystemdUnitPutsStartLimitsInUnitSection(t *testing.T) {
	u := systemdUnit()
	for _, key := range []string{"StartLimitIntervalSec", "StartLimitBurst"} {
		if got := sectionOf(u, key); got != "Unit" {
			t.Errorf("%s in [%s], want [Unit]", key, got)
		}
	}
}

func TestSystemdUnitDefinesHome(t *testing.T) {
	if !strings.Contains(systemdUnit(), "Environment=HOME=") {
		t.Error("unit must define HOME; systemd does not set it for root services")
	}
}
