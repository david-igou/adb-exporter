// Command adb-exporter is a Prometheus exporter that scrapes an Android device
// over adb by shelling out to the adb CLI and parsing a fixed set of shell
// commands. All adb access is serialized; metrics are collected per scrape.
package main

import (
	"flag"
	"log"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/david-igou/adb-exporter/internal/adb"
	"github.com/david-igou/adb-exporter/internal/collector"
)

// Build metadata, overridable via -ldflags "-X main.version=... -X main.revision=...".
var (
	version  = "dev"
	revision = "unknown"
)

func main() {
	var (
		listenAddr     = flag.String("web.listen-address", ":9836", "HTTP listen address for /metrics.")
		adbAddress     = flag.String("adb.address", "10.10.9.22:5555", "host:port passed to `adb -s`.")
		adbPath        = flag.String("adb.path", "adb", "Path to the adb binary.")
		adbTimeout     = flag.Duration("adb.timeout", 5*time.Second, "Per-command context timeout. Overall scrape cap = 8x.")
		topProcesses   = flag.Int("collect.top-processes", 15, "Emit the top-N processes by RSS.")
		processInclude = flag.String("collect.process-include", "", "Comma-separated process names always emitted regardless of rank.")
	)
	flag.Parse()

	include := splitCSV(*processInclude)
	client := adb.NewClient(*adbPath, *adbAddress, *adbTimeout)
	overallTimeout := 8 * *adbTimeout

	build := collector.BuildInfo{
		Version:   version,
		Revision:  revision,
		GoVersion: runtime.Version(),
	}

	c := collector.New(client, build, overallTimeout, *topProcesses, include)

	mux := http.NewServeMux()
	// Build a fresh registry per request so the collector is bound to the request
	// context: when Prometheus abandons a slow scrape, the adb work is cancelled
	// and queued scrapes drain instead of piling up on the collector mutex.
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		reg := prometheus.NewRegistry()
		reg.MustRegister(c.ForRequest(r.Context()))
		promhttp.HandlerFor(reg, promhttp.HandlerOpts{}).ServeHTTP(w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html>
<head><title>adb-exporter</title></head>
<body><h1>adb-exporter</h1><p><a href="/metrics">Metrics</a></p></body>
</html>`))
	})

	srv := &http.Server{
		Addr:              *listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("adb-exporter %s (%s) listening on %s, target %s", version, revision, *listenAddr, *adbAddress)
	log.Fatal(srv.ListenAndServe())
}

// splitCSV splits a comma-separated list, trimming spaces and dropping empties.
func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
