package collector

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/david-igou/adb-exporter/internal/adb"
)

// netdevStats holds the four rx/tx counter pairs we export for one interface.
type netdevStats struct {
	Iface string
	// Receive: bytes, packets, errs, drop, fifo, frame, compressed, multicast.
	RxBytes, RxPackets, RxErrs, RxDrop float64
	// Transmit: bytes, packets, errs, drop, fifo, colls, carrier, compressed.
	TxBytes, TxPackets, TxErrs, TxDrop float64
}

// netdevCollector emits adb_network_* counters from /proc/net/dev.
type netdevCollector struct{}

// Name implements SubCollector.
func (n *netdevCollector) Name() string { return "netdev" }

var (
	netBytesDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "network", "bytes_total"),
		"Network bytes transferred.", []string{"interface", "direction"}, nil)
	netPacketsDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "network", "packets_total"),
		"Network packets transferred.", []string{"interface", "direction"}, nil)
	netErrsDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "network", "errs_total"),
		"Network errors.", []string{"interface", "direction"}, nil)
	netDropDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "network", "drop_total"),
		"Network dropped packets.", []string{"interface", "direction"}, nil)
)

// Collect implements SubCollector.
func (n *netdevCollector) Collect(ctx context.Context, client *adb.Client, ch chan<- prometheus.Metric) error {
	out, err := client.RunShell(ctx, "cat /proc/net/dev")
	if err != nil {
		return err
	}
	stats, err := parseNetdev(out)
	if err != nil {
		return err
	}
	for _, s := range stats {
		emit := func(desc *prometheus.Desc, rx, tx float64) {
			ch <- prometheus.MustNewConstMetric(desc, prometheus.CounterValue, rx, s.Iface, "rx")
			ch <- prometheus.MustNewConstMetric(desc, prometheus.CounterValue, tx, s.Iface, "tx")
		}
		emit(netBytesDesc, s.RxBytes, s.TxBytes)
		emit(netPacketsDesc, s.RxPackets, s.TxPackets)
		emit(netErrsDesc, s.RxErrs, s.TxErrs)
		emit(netDropDesc, s.RxDrop, s.TxDrop)
	}
	return nil
}

// parseNetdev parses /proc/net/dev. The first two header lines are skipped;
// each remaining line is split on ':' into an interface name and 16
// whitespace-separated counters.
func parseNetdev(out string) ([]netdevStats, error) {
	var stats []netdevStats
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if i < 2 {
			continue // two header lines
		}
		line = strings.TrimRight(line, "\r")
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		iface := strings.TrimSpace(line[:colon])
		fields := strings.Fields(line[colon+1:])
		if iface == "" || len(fields) < 16 {
			continue
		}
		vals := make([]float64, 16)
		ok := true
		for j := 0; j < 16; j++ {
			v, err := strconv.ParseFloat(fields[j], 64)
			if err != nil {
				ok = false
				break
			}
			vals[j] = v
		}
		if !ok {
			continue
		}
		stats = append(stats, netdevStats{
			Iface:     iface,
			RxBytes:   vals[0],
			RxPackets: vals[1],
			RxErrs:    vals[2],
			RxDrop:    vals[3],
			TxBytes:   vals[8],
			TxPackets: vals[9],
			TxErrs:    vals[10],
			TxDrop:    vals[11],
		})
	}
	if len(stats) == 0 {
		return nil, fmt.Errorf("netdev: no parseable interface lines")
	}
	return stats, nil
}
