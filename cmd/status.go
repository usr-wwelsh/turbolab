package cmd

import "github.com/usr-wwelsh/turbolab/internal/monitor"

func ramFields(stats *monitor.Stats) map[string]float64 {
	if stats == nil {
		return map[string]float64{"ram_available_gb": 0, "ram_used_percent": 0}
	}
	return map[string]float64{
		"ram_available_gb": stats.AvailableGB(),
		"ram_used_percent": stats.UsedPercent(),
	}
}
