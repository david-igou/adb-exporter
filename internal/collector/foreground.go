package collector

import (
	"context"
	"regexp"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/david-igou/adb-exporter/internal/adb"
)

// foregroundCollector emits adb_foreground_app_info for the resumed activity.
type foregroundCollector struct{}

// Name implements SubCollector.
func (f *foregroundCollector) Name() string { return "foreground" }

var foregroundDesc = prometheus.NewDesc(
	prometheus.BuildFQName(namespace, "foreground", "app_info"),
	"Info metric (always 1) labelled with the current foreground app package and activity.",
	[]string{"package", "activity"}, nil)

// resumedActivityRe captures the package and activity from a ResumedActivity
// ActivityRecord line, e.g.
//
//	ResumedActivity: ActivityRecord{903e9f8 u0 com.spocky.projengmenu/.ui.home.MainActivity t1554}
var resumedActivityRe = regexp.MustCompile(`ResumedActivity:.*\bu\d+ (\S+?)/(\S+)`)

// Collect implements SubCollector.
func (f *foregroundCollector) Collect(ctx context.Context, client *adb.Client, ch chan<- prometheus.Metric) error {
	// `|| true` keeps the device shell exit code 0 when grep matches nothing, so
	// "no foreground app" is an empty result rather than a command failure.
	out, err := client.RunShell(ctx, "dumpsys activity activities | grep -m1 ResumedActivity || true")
	if err != nil {
		return err
	}
	pkg, activity, found := parseForeground(out)
	if !found {
		return nil // no resumed activity is not an error
	}
	ch <- prometheus.MustNewConstMetric(foregroundDesc, prometheus.GaugeValue, 1, pkg, activity)
	return nil
}

// parseForeground extracts the package and activity from a ResumedActivity line.
// The returned bool is false when no ResumedActivity line is present.
func parseForeground(out string) (pkg, activity string, found bool) {
	m := resumedActivityRe.FindStringSubmatch(out)
	if m == nil {
		return "", "", false
	}
	// Strip a trailing "}" or " t<id>}" that may follow the activity token.
	act := m[2]
	if i := strings.IndexAny(act, " }"); i >= 0 {
		act = act[:i]
	}
	return m[1], act, true
}
