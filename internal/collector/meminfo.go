package collector

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/david-igou/adb-exporter/internal/adb"
)

// meminfoKeys maps the /proc/meminfo keys we export to their metric subsystem
// name. Only these keys are emitted; all others are ignored.
var meminfoKeys = []struct {
	key    string
	metric string
	help   string
}{
	{"MemTotal", "total_bytes", "Total usable RAM in bytes."},
	{"MemFree", "free_bytes", "Free RAM in bytes."},
	{"MemAvailable", "available_bytes", "Estimated available memory in bytes."},
	{"Buffers", "buffers_bytes", "Memory in buffer cache in bytes."},
	{"Cached", "cached_bytes", "Memory in the page cache in bytes."},
	{"SwapTotal", "swap_total_bytes", "Total swap in bytes."},
	{"SwapFree", "swap_free_bytes", "Free swap in bytes."},
}

// meminfoCollector emits adb_memory_* gauges from /proc/meminfo.
type meminfoCollector struct{}

// Name implements SubCollector.
func (m *meminfoCollector) Name() string { return "meminfo" }

// Collect implements SubCollector.
func (m *meminfoCollector) Collect(ctx context.Context, client *adb.Client, ch chan<- prometheus.Metric) error {
	out, err := client.RunShell(ctx, "cat /proc/meminfo")
	if err != nil {
		return err
	}
	values, err := parseMeminfo(out)
	if err != nil {
		return err
	}
	for _, k := range meminfoKeys {
		v, ok := values[k.key]
		if !ok {
			continue // missing key is not an error
		}
		desc := prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "memory", k.metric), k.help, nil, nil)
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, v)
	}
	return nil
}

// parseMeminfo parses /proc/meminfo output, returning a map of key -> bytes
// (kB values multiplied by 1024) for every "<key>: <n> kB" line. It errors only
// when the output contains no parseable line at all.
func parseMeminfo(out string) (map[string]float64, error) {
	values := make(map[string]float64)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		fields := strings.Fields(line)
		// Expected form: "MemTotal:", "3016708", "kB"
		if len(fields) < 3 || fields[2] != "kB" {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		kb, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			continue
		}
		values[key] = kb * 1024
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("meminfo: no parseable lines in %q", out)
	}
	return values, nil
}
