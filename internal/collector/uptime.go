package collector

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/david-igou/adb-exporter/internal/adb"
)

// uptimeCollector emits adb_uptime_seconds from /proc/uptime.
type uptimeCollector struct{}

// Name implements SubCollector.
func (u *uptimeCollector) Name() string { return "uptime" }

var uptimeDesc = prometheus.NewDesc(
	prometheus.BuildFQName(namespace, "", "uptime_seconds"),
	"Device uptime in seconds.", nil, nil)

// Collect implements SubCollector.
func (u *uptimeCollector) Collect(ctx context.Context, client *adb.Client, ch chan<- prometheus.Metric) error {
	out, err := client.RunShell(ctx, "cat /proc/uptime")
	if err != nil {
		return err
	}
	seconds, err := parseUptime(out)
	if err != nil {
		return err
	}
	ch <- prometheus.MustNewConstMetric(uptimeDesc, prometheus.GaugeValue, seconds)
	return nil
}

// parseUptime parses the first field of /proc/uptime as uptime seconds.
func parseUptime(out string) (float64, error) {
	fields := strings.Fields(strings.TrimRight(out, "\r\n"))
	if len(fields) < 1 {
		return 0, fmt.Errorf("uptime: empty output %q", out)
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("uptime: field %q: %w", fields[0], err)
	}
	return v, nil
}
