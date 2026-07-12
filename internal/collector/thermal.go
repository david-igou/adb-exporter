package collector

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/david-igou/adb-exporter/internal/adb"
)

// thermalTypeNames maps Android Temperature.Type ints to label values.
var thermalTypeNames = map[int]string{
	0: "CPU",
	1: "GPU",
	2: "BATTERY",
	3: "SKIN",
	4: "USB_PORT",
	5: "POWER_AMPLIFIER",
	6: "BCL_VOLTAGE",
	7: "BCL_CURRENT",
	8: "BCL_PERCENTAGE",
	9: "NPU",
}

// thermalTypeLabel maps an mType int to its label, defaulting to "unknown".
func thermalTypeLabel(t int) string {
	if name, ok := thermalTypeNames[t]; ok {
		return name
	}
	return "unknown"
}

// tempReading is one parsed Temperature{...} entry.
type tempReading struct {
	Value float64
	Type  int
	Name  string
}

// thermalCollector emits adb_thermal_temperature_celsius from dumpsys thermalservice.
type thermalCollector struct{}

// Name implements SubCollector.
func (t *thermalCollector) Name() string { return "thermal" }

var thermalDesc = prometheus.NewDesc(
	prometheus.BuildFQName(namespace, "thermal", "temperature_celsius"),
	"Device thermal sensor reading in degrees Celsius.",
	[]string{"name", "type"}, nil)

// temperatureRe extracts mValue, mType and mName from a Temperature{...} line.
var temperatureRe = regexp.MustCompile(`mValue=([-\d.]+).*mType=(\d+).*mName=([^,}]+)`)

// Collect implements SubCollector.
func (t *thermalCollector) Collect(ctx context.Context, client *adb.Client, ch chan<- prometheus.Metric) error {
	out, err := client.RunShell(ctx, "dumpsys thermalservice")
	if err != nil {
		return err
	}
	readings, err := parseThermal(out)
	if err != nil {
		return err
	}
	for _, r := range readings {
		ch <- prometheus.MustNewConstMetric(
			thermalDesc, prometheus.GaugeValue, r.Value, r.Name, thermalTypeLabel(r.Type))
	}
	return nil
}

// parseThermal parses `dumpsys thermalservice` output. It reads the
// "Current temperatures from HAL:" block, falling back to
// "Cached temperatures:" if the HAL block is absent or yields nothing.
func parseThermal(out string) ([]tempReading, error) {
	readings := parseThermalBlock(out, "Current temperatures from HAL:")
	if len(readings) == 0 {
		readings = parseThermalBlock(out, "Cached temperatures:")
	}
	if len(readings) == 0 {
		return nil, fmt.Errorf("thermal: no Temperature entries found")
	}
	return dedupeReadings(readings), nil
}

// dedupeReadings drops readings whose emitted {name, type} label pair repeats.
// A HAL that reports two sensors with the same mName and same mapped type label
// would otherwise produce two identical adb_thermal_temperature_celsius series
// and fail prometheus Gather (HTTP 500 for the whole scrape). The mapped type
// label is used as the key (not the raw mType) since distinct unknown types
// collapse to the same "unknown" label. First occurrence wins.
func dedupeReadings(in []tempReading) []tempReading {
	if len(in) < 2 {
		return in
	}
	type key struct{ name, typ string }
	seen := make(map[key]bool, len(in))
	out := make([]tempReading, 0, len(in))
	for _, r := range in {
		k := key{name: r.Name, typ: thermalTypeLabel(r.Type)}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, r)
	}
	return out
}

// parseThermalBlock collects Temperature{...} readings from the block that
// begins at the line equal to header, stopping at the first following line that
// is not a Temperature entry.
func parseThermalBlock(out, header string) []tempReading {
	var readings []tempReading
	lines := strings.Split(out, "\n")
	inBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r"))
		if !inBlock {
			if trimmed == header {
				inBlock = true
			}
			continue
		}
		if !strings.HasPrefix(trimmed, "Temperature{") {
			break // end of block
		}
		m := temperatureRe.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}
		value, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			continue
		}
		typ, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		readings = append(readings, tempReading{
			Value: value,
			Type:  typ,
			Name:  strings.TrimSpace(m[3]),
		})
	}
	return readings
}
