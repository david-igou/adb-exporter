package collector

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/david-igou/adb-exporter/internal/adb"
)

// filesystemStats holds the parsed df row for one mountpoint. Sizes are in
// bytes (1K-blocks multiplied by 1024).
type filesystemStats struct {
	Mountpoint string
	SizeBytes  float64
	UsedBytes  float64
	AvailBytes float64
}

// storageCollector emits adb_filesystem_* gauges from `df /data /cache`.
type storageCollector struct{}

// Name implements SubCollector.
func (s *storageCollector) Name() string { return "storage" }

var (
	fsSizeDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "filesystem", "size_bytes"),
		"Filesystem size in bytes.", []string{"mountpoint"}, nil)
	fsUsedDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "filesystem", "used_bytes"),
		"Filesystem used space in bytes.", []string{"mountpoint"}, nil)
	fsAvailDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "filesystem", "avail_bytes"),
		"Filesystem available space in bytes.", []string{"mountpoint"}, nil)
)

// Collect implements SubCollector.
func (s *storageCollector) Collect(ctx context.Context, client *adb.Client, ch chan<- prometheus.Metric) error {
	// toybox df does not support -B1; default output is 1K-blocks. A missing path
	// (e.g. a device without a separate /cache) makes df exit non-zero, which
	// would otherwise discard the stdout rows for the paths that DID resolve;
	// `|| true` forces exit 0 so those rows are still parsed. parseDF still errors
	// if nothing is parseable.
	out, err := client.RunShell(ctx, "df /data /cache 2>/dev/null || true")
	if err != nil {
		return err
	}
	rows, err := parseDF(out)
	if err != nil {
		return err
	}
	for _, r := range rows {
		ch <- prometheus.MustNewConstMetric(fsSizeDesc, prometheus.GaugeValue, r.SizeBytes, r.Mountpoint)
		ch <- prometheus.MustNewConstMetric(fsUsedDesc, prometheus.GaugeValue, r.UsedBytes, r.Mountpoint)
		ch <- prometheus.MustNewConstMetric(fsAvailDesc, prometheus.GaugeValue, r.AvailBytes, r.Mountpoint)
	}
	return nil
}

// parseDF parses toybox `df` default (1K-block) output. The header line
// (starting with "Filesystem") is skipped; each data row splits into six
// whitespace-separated fields [Filesystem, 1K-blocks, Used, Available, Use%,
// Mounted-on]. Block counts are multiplied by 1024 to bytes; the mountpoint
// label is the reported "Mounted on" column verbatim.
func parseDF(out string) ([]filesystemStats, error) {
	var rows []filesystemStats
	// seen dedupes by mountpoint: two path args can resolve to the same mount
	// (e.g. /cache sharing /data's partition), which would emit duplicate
	// adb_filesystem_* series and fail prometheus Gather. First row wins.
	seen := make(map[string]bool)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		fields := strings.Fields(line)
		if len(fields) != 6 {
			continue
		}
		if fields[0] == "Filesystem" {
			continue // header
		}
		size, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			continue
		}
		used, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			continue
		}
		avail, err := strconv.ParseFloat(fields[3], 64)
		if err != nil {
			continue
		}
		if seen[fields[5]] {
			continue
		}
		seen[fields[5]] = true
		rows = append(rows, filesystemStats{
			Mountpoint: fields[5],
			SizeBytes:  size * 1024,
			UsedBytes:  used * 1024,
			AvailBytes: avail * 1024,
		})
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("df: no parseable rows in %q", out)
	}
	return rows, nil
}
