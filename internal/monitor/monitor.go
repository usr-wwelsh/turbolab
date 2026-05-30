package monitor

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

type Stats struct {
	TotalRAM     uint64
	AvailableRAM uint64
	UsedRAM      uint64
}

func (s Stats) AvailableGB() float64 {
	return float64(s.AvailableRAM) / 1024 / 1024 / 1024
}

func (s Stats) UsedPercent() float64 {
	if s.TotalRAM == 0 {
		return 0
	}
	return float64(s.UsedRAM) / float64(s.TotalRAM) * 100
}

func Get() (*Stats, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return nil, fmt.Errorf("meminfo: %w", err)
	}
	defer f.Close()

	fields := map[string]uint64{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSuffix(parts[0], ":")
		val, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			continue
		}
		fields[key] = val * 1024 // kB → bytes
	}

	total := fields["MemTotal"]
	avail := fields["MemAvailable"]
	return &Stats{
		TotalRAM:     total,
		AvailableRAM: avail,
		UsedRAM:      total - avail,
	}, nil
}

var (
	cpuMu        sync.Mutex
	prevCPUIdle  uint64
	prevCPUTotal uint64
)

func CPUPercent() (float64, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:] // skip "cpu"
		var vals [10]uint64
		for i := 0; i < len(fields) && i < 10; i++ {
			vals[i], _ = strconv.ParseUint(fields[i], 10, 64)
		}
		// user, nice, system, idle, iowait, irq, softirq, steal, guest, guest_nice
		idle := vals[3] + vals[4]
		total := uint64(0)
		for _, v := range vals {
			total += v
		}

		cpuMu.Lock()
		pct := float64(0)
		if prevCPUTotal > 0 && total > prevCPUTotal {
			idleDelta := idle - prevCPUIdle
			totalDelta := total - prevCPUTotal
			pct = (1 - float64(idleDelta)/float64(totalDelta)) * 100
		}
		prevCPUIdle = idle
		prevCPUTotal = total
		cpuMu.Unlock()

		return pct, nil
	}
	return 0, fmt.Errorf("cpu line not found in /proc/stat")
}

// ProcessRSS returns the resident set size (bytes) of a process by PID,
// read from /proc/<pid>/statm (field 2 = resident pages).
func ProcessRSS(pid int) (uint64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/statm", pid))
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0, fmt.Errorf("unexpected statm format for pid %d", pid)
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, err
	}
	return pages * uint64(os.Getpagesize()), nil
}

// WillOOM returns true if loading a model of modelBytes would exhaust available RAM.
func WillOOM(modelBytes uint64) (bool, error) {
	stats, err := Get()
	if err != nil {
		return false, err
	}
	// Keep 1GB headroom
	const headroom = 1 << 30
	return modelBytes+headroom > stats.AvailableRAM, nil
}

type DiskSpace struct {
	UsedGB      float64
	AvailableGB float64
	TotalGB     float64
}

func DiskUsage(path string) (*DiskSpace, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil, err
	}

	blockSize := uint64(stat.Bsize)
	total := stat.Blocks * blockSize
	available := stat.Bavail * blockSize
	used := total - (stat.Bfree * blockSize)

	return &DiskSpace{
		UsedGB:      float64(used) / 1024 / 1024 / 1024,
		AvailableGB: float64(available) / 1024 / 1024 / 1024,
		TotalGB:     float64(total) / 1024 / 1024 / 1024,
	}, nil
}
