// Package collector implements the per-scrape prometheus.Collector for
// adb-exporter and the individual device sub-collectors.
//
// The top-level Collector holds a serialized adb.Client, runs each
// sub-collector sequentially (never in parallel), and owns the scrape-meta
// metrics (adb_up, build_info, scrape duration, per-collector success, and the
// lifetime error counter). Sub-collectors receive the already-serialized client
// and never touch adb_up or the mutex directly.
package collector

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/david-igou/adb-exporter/internal/adb"
)

// namespace prefixes every metric emitted by the exporter.
const namespace = "adb"

// SubCollector is a single device metric family. Implementations parse the
// output of one or more adb shell commands and emit their metrics on ch. A
// returned error marks the sub-collector as failed for the scrape but must not
// affect the others.
type SubCollector interface {
	// Name is the stable identifier used as the `collector` label on
	// adb_scrape_collector_success.
	Name() string
	// Collect runs the sub-collector's source command(s) through client and
	// emits its metrics. It returns an error on command failure or unexpected
	// output. Documented idle cases (no media playback, no foreground app) emit
	// fewer series and return nil.
	Collect(ctx context.Context, client *adb.Client, ch chan<- prometheus.Metric) error
}

// BuildInfo carries the ldflags-injected build metadata for adb_exporter_build_info.
type BuildInfo struct {
	Version   string
	Revision  string
	GoVersion string
}

// Collector is the top-level prometheus.Collector. It is safe for concurrent
// scrapes: Collect holds a mutex so scrapes never overlap, matching the
// device's serialization requirement.
type Collector struct {
	client   *adb.Client
	build    BuildInfo
	timeout  time.Duration
	children []SubCollector

	mu        sync.Mutex
	errsTotal float64

	// Descriptors owned by the top-level collector.
	upDesc              *prometheus.Desc
	buildInfoDesc       *prometheus.Desc
	scrapeDurationDesc  *prometheus.Desc
	collectorSuccessDsc *prometheus.Desc
	scrapeErrorsDesc    *prometheus.Desc
}

// New returns a Collector wired to client with the fixed, ordered set of device
// sub-collectors. topProcesses and processInclude configure the process
// sub-collector; overallTimeout bounds the entire scrape.
func New(client *adb.Client, build BuildInfo, overallTimeout time.Duration, topProcesses int, processInclude []string) *Collector {
	return &Collector{
		client:   client,
		build:    build,
		timeout:  overallTimeout,
		children: defaultSubCollectors(topProcesses, processInclude),

		upDesc: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "up"),
			"1 if the device is reachable (adb state == device), else 0.",
			nil, nil),
		buildInfoDesc: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "exporter", "build_info"),
			"Build metadata for adb-exporter.",
			[]string{"version", "revision", "goversion"}, nil),
		scrapeDurationDesc: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "scrape", "duration_seconds"),
			"Wall-clock duration of the whole scrape.",
			nil, nil),
		collectorSuccessDsc: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "scrape", "collector_success"),
			"1 if the sub-collector succeeded this scrape, else 0.",
			[]string{"collector"}, nil),
		scrapeErrorsDesc: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "scrape", "errors_total"),
			"Process-lifetime count of sub-collector and reconnect failures.",
			nil, nil),
	}
}

// defaultSubCollectors returns the device sub-collectors in their fixed
// collection order.
func defaultSubCollectors(topProcesses int, processInclude []string) []SubCollector {
	return []SubCollector{
		&meminfoCollector{},
		&loadavgCollector{},
		&uptimeCollector{},
		newProcessCollector(topProcesses, processInclude),
		&thermalCollector{},
		&netdevCollector{},
		&storageCollector{},
		&mediaSessionCollector{},
		&foregroundCollector{},
	}
}

// Describe implements prometheus.Collector. The exporter is an unchecked
// collector because label sets (process names, interfaces, mountpoints) are
// discovered at scrape time, so only the meta descriptors are sent.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.upDesc
	ch <- c.buildInfoDesc
	ch <- c.scrapeDurationDesc
	ch <- c.collectorSuccessDsc
	ch <- c.scrapeErrorsDesc
}

// ForRequest returns a prometheus.Collector that runs one scrape bound to reqCtx
// (typically the HTTP request context). Threading the request context means that
// when Prometheus abandons a slow scrape at its scrape_timeout, the in-flight adb
// commands are cancelled and any scrape queued behind the shared mutex drains
// fast instead of running to completion — preventing an unbounded goroutine/
// request pile-up against a persistently slow device. Register it in a
// per-request registry; it shares this Collector's mutex and lifetime counters.
func (c *Collector) ForRequest(reqCtx context.Context) prometheus.Collector {
	return requestCollector{c: c, ctx: reqCtx}
}

// requestCollector binds a single scrape to a request context, delegating the
// descriptors and shared state to the underlying Collector.
type requestCollector struct {
	c   *Collector
	ctx context.Context
}

func (r requestCollector) Describe(ch chan<- *prometheus.Desc) { r.c.Describe(ch) }
func (r requestCollector) Collect(ch chan<- prometheus.Metric) { r.c.collect(r.ctx, ch) }

// Collect implements prometheus.Collector using a background context. Prefer
// ForRequest so an abandoned scrape cancels its adb work; this direct path
// exists so Collector still satisfies prometheus.Collector for registration.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	c.collect(context.Background(), ch)
}

// collect runs one full scrape under reqCtx: ensure the device is connected,
// then run each sub-collector sequentially, always emitting a collector_success
// sample per child. It never panics and always emits the meta metrics. The
// per-scrape overall timeout is applied on top of reqCtx.
func (c *Collector) collect(reqCtx context.Context, ch chan<- prometheus.Metric) {
	c.mu.Lock()
	defer c.mu.Unlock()

	start := time.Now()

	ch <- prometheus.MustNewConstMetric(
		c.buildInfoDesc, prometheus.GaugeValue, 1,
		c.build.Version, c.build.Revision, c.build.GoVersion)

	ctx, cancel := context.WithTimeout(reqCtx, c.timeout)
	defer cancel()

	up, err := c.client.EnsureConnected(ctx)
	if err != nil {
		log.Printf("adb-exporter: device unreachable: %v", err)
		c.errsTotal++
	}

	var upVal float64
	if up {
		upVal = 1
	}
	ch <- prometheus.MustNewConstMetric(c.upDesc, prometheus.GaugeValue, upVal)

	// Run each sub-collector. When the device is down every child reports 0 and
	// no device metrics are emitted.
	for _, child := range c.children {
		success := 0.0
		if up {
			if cErr := child.Collect(ctx, c.client, ch); cErr != nil {
				log.Printf("adb-exporter: collector %q failed: %v", child.Name(), cErr)
				c.errsTotal++
			} else {
				success = 1
			}
		}
		ch <- prometheus.MustNewConstMetric(
			c.collectorSuccessDsc, prometheus.GaugeValue, success, child.Name())
	}

	ch <- prometheus.MustNewConstMetric(
		c.scrapeErrorsDesc, prometheus.CounterValue, c.errsTotal)
	ch <- prometheus.MustNewConstMetric(
		c.scrapeDurationDesc, prometheus.GaugeValue, time.Since(start).Seconds())
}
