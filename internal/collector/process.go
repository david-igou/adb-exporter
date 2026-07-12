package collector

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/david-igou/adb-exporter/internal/adb"
)

// processInfo is one parsed row of `ps -A -o PID,RSS,%CPU,NAME`.
type processInfo struct {
	PID     int
	RSSkB   int64
	CPUPerc float64
	Name    string
}

// processCollector emits per-process RSS and CPU for the top-N processes by RSS
// plus an allowlist of names.
type processCollector struct {
	topN    int
	include map[string]bool
}

// newProcessCollector builds a processCollector for the top-N processes by RSS,
// always additionally emitting any process whose NAME is in include.
func newProcessCollector(topN int, include []string) *processCollector {
	set := make(map[string]bool, len(include))
	for _, name := range include {
		name = strings.TrimSpace(name)
		if name != "" {
			set[name] = true
		}
	}
	return &processCollector{topN: topN, include: set}
}

// Name implements SubCollector.
func (p *processCollector) Name() string { return "process" }

var (
	processRSSDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "process", "memory_rss_bytes"),
		"Resident set size of the process in bytes.",
		[]string{"process"}, nil)
	processCPUDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "process", "cpu_ratio"),
		"Process CPU usage as a ratio 0..1 (toybox ps %CPU is a lifetime average).",
		[]string{"process"}, nil)
)

// Collect implements SubCollector.
func (p *processCollector) Collect(ctx context.Context, client *adb.Client, ch chan<- prometheus.Metric) error {
	out, err := client.RunShell(ctx, "ps -A -o PID,RSS,%CPU,NAME")
	if err != nil {
		return err
	}
	procs, err := parsePS(out)
	if err != nil {
		return err
	}
	for _, proc := range p.selectProcesses(procs) {
		ch <- prometheus.MustNewConstMetric(
			processRSSDesc, prometheus.GaugeValue, float64(proc.RSSkB)*1024, proc.Name)
		ch <- prometheus.MustNewConstMetric(
			processCPUDesc, prometheus.GaugeValue, proc.CPUPerc/100, proc.Name)
	}
	return nil
}

// selectProcesses returns the processes to emit: the top-N by RSS plus any
// allowlisted names, deduplicated by NAME keeping the highest-RSS instance.
func (p *processCollector) selectProcesses(procs []processInfo) []processInfo {
	// Sort by RSS descending; stable so equal-RSS order is deterministic.
	sorted := make([]processInfo, len(procs))
	copy(sorted, procs)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].RSSkB > sorted[j].RSSkB
	})

	// byName keeps the highest-RSS instance per name (input is RSS-desc sorted,
	// so the first occurrence wins).
	byName := make(map[string]processInfo)
	order := make([]string, 0)
	// addName records the first (highest-RSS) instance of a name, returning true
	// only when the name was newly added.
	addName := func(proc processInfo) bool {
		if _, seen := byName[proc.Name]; seen {
			return false
		}
		byName[proc.Name] = proc
		order = append(order, proc.Name)
		return true
	}

	// Count DISTINCT names toward the top-N budget, not sorted rows: duplicate
	// names within the first topN rows (e.g. two gms.persistent PIDs) must not
	// consume a slot, or fewer than topN distinct processes would be emitted.
	// Once the budget is spent, only allowlisted names are still added.
	distinct := 0
	for _, proc := range sorted {
		switch {
		case distinct < p.topN:
			if addName(proc) {
				distinct++
			}
		case p.include[proc.Name]:
			addName(proc)
		}
	}

	out := make([]processInfo, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}
	return out
}

// parsePS parses `ps -A -o PID,RSS,%CPU,NAME` output. The header line (starting
// with "PID") is skipped; each remaining row must split into exactly four
// whitespace-separated fields. Process names never contain spaces on the
// reference device, so 4-field splitting is safe.
func parsePS(out string) ([]processInfo, error) {
	var procs []processInfo
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		fields := strings.Fields(line)
		if len(fields) != 4 {
			continue
		}
		if fields[0] == "PID" {
			continue // header
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		rss, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		cpu, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			continue
		}
		procs = append(procs, processInfo{
			PID:     pid,
			RSSkB:   rss,
			CPUPerc: cpu,
			Name:    fields[3],
		})
	}
	if len(procs) == 0 {
		return nil, fmt.Errorf("ps: no parseable rows in %q", out)
	}
	return procs, nil
}
