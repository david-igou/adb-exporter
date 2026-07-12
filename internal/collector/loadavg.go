package collector

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/david-igou/adb-exporter/internal/adb"
)

// loadavgCollector emits adb_load1/5/15 from /proc/loadavg.
type loadavgCollector struct{}

// Name implements SubCollector.
func (l *loadavgCollector) Name() string { return "loadavg" }

var loadavgDescs = [3]*prometheus.Desc{
	prometheus.NewDesc(prometheus.BuildFQName(namespace, "", "load1"), "1-minute load average.", nil, nil),
	prometheus.NewDesc(prometheus.BuildFQName(namespace, "", "load5"), "5-minute load average.", nil, nil),
	prometheus.NewDesc(prometheus.BuildFQName(namespace, "", "load15"), "15-minute load average.", nil, nil),
}

// Collect implements SubCollector.
func (l *loadavgCollector) Collect(ctx context.Context, client *adb.Client, ch chan<- prometheus.Metric) error {
	out, err := client.RunShell(ctx, "cat /proc/loadavg")
	if err != nil {
		return err
	}
	loads, err := parseLoadavg(out)
	if err != nil {
		return err
	}
	for i, v := range loads {
		ch <- prometheus.MustNewConstMetric(loadavgDescs[i], prometheus.GaugeValue, v)
	}
	return nil
}

// parseLoadavg parses the first three whitespace-separated fields of
// /proc/loadavg as the 1/5/15-minute load averages.
func parseLoadavg(out string) ([3]float64, error) {
	var loads [3]float64
	fields := strings.Fields(strings.TrimRight(out, "\r\n"))
	if len(fields) < 3 {
		return loads, fmt.Errorf("loadavg: expected >=3 fields, got %d in %q", len(fields), out)
	}
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return loads, fmt.Errorf("loadavg: field %d %q: %w", i, fields[i], err)
		}
		loads[i] = v
	}
	return loads, nil
}
