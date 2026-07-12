package collector

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/david-igou/adb-exporter/internal/adb"
)

// playbackState is one session's active PlaybackState.
type playbackState struct {
	Package string
	State   int
}

// mediaSessionResult is the parsed output of dumpsys media_session.
type mediaSessionResult struct {
	Count     int
	Playbacks []playbackState
}

// mediaSessionCollector emits adb_media_session_count and, for actively playing
// sessions, adb_media_playback_state.
type mediaSessionCollector struct{}

// Name implements SubCollector.
func (m *mediaSessionCollector) Name() string { return "mediasession" }

var (
	mediaCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "media", "session_count"),
		"Number of registered media sessions.", nil, nil)
	mediaPlaybackDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "media", "playback_state"),
		"Active PlaybackState int (3=PLAYING, 2=PAUSED, ...) per session package.",
		[]string{"package"}, nil)
)

var (
	sessionCountRe   = regexp.MustCompile(`Sessions Stack - have (\d+) sessions:`)
	sessionPackageRe = regexp.MustCompile(`^package=(\S+)`)
	playbackStateRe  = regexp.MustCompile(`state=PlaybackState \{state=(\d+)`)
)

// Collect implements SubCollector.
func (m *mediaSessionCollector) Collect(ctx context.Context, client *adb.Client, ch chan<- prometheus.Metric) error {
	out, err := client.RunShell(ctx, "dumpsys media_session")
	if err != nil {
		return err
	}
	result := parseMediaSession(out)
	ch <- prometheus.MustNewConstMetric(mediaCountDesc, prometheus.GaugeValue, float64(result.Count))
	for _, pb := range result.Playbacks {
		ch <- prometheus.MustNewConstMetric(
			mediaPlaybackDesc, prometheus.GaugeValue, float64(pb.State), pb.Package)
	}
	return nil
}

// parseMediaSession parses dumpsys media_session output. The session count comes
// from the "Sessions Stack - have N sessions:" line, falling back to a count of
// "package=" lines. Each session block is delimited by a "package=" line; an
// active "state=PlaybackState {state=N ...}" line for that session yields a
// playback entry. "state=null" sessions (idle) emit no playback metric and are
// not an error.
func parseMediaSession(out string) mediaSessionResult {
	var result mediaSessionResult
	var packageLines int
	var currentPkg string

	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r"))

		if m := sessionCountRe.FindStringSubmatch(trimmed); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				result.Count = n
			}
			continue
		}
		if m := sessionPackageRe.FindStringSubmatch(trimmed); m != nil {
			currentPkg = m[1]
			packageLines++
			continue
		}
		if m := playbackStateRe.FindStringSubmatch(trimmed); m != nil && currentPkg != "" {
			if state, err := strconv.Atoi(m[1]); err == nil {
				result.Playbacks = append(result.Playbacks, playbackState{
					Package: currentPkg,
					State:   state,
				})
			}
		}
	}

	// Fallback: if the "Sessions Stack" phrase was absent, count package lines.
	if result.Count == 0 {
		result.Count = packageLines
	}

	// Dedupe by package: a package may register multiple active sessions (e.g. a
	// player plus its Cast/companion session), which would otherwise emit two
	// adb_media_playback_state series with the same {package} label and fail
	// prometheus Gather (HTTP 500 for the whole scrape). Keep the highest State
	// per package (prefer PLAYING over PAUSED), preserving first-seen order.
	result.Playbacks = dedupePlaybacks(result.Playbacks)
	return result
}

// dedupePlaybacks collapses multiple sessions sharing a package into one entry,
// keeping the highest State value and the order of first appearance.
func dedupePlaybacks(in []playbackState) []playbackState {
	if len(in) < 2 {
		return in
	}
	idx := make(map[string]int, len(in))
	out := make([]playbackState, 0, len(in))
	for _, pb := range in {
		if i, seen := idx[pb.Package]; seen {
			if pb.State > out[i].State {
				out[i].State = pb.State
			}
			continue
		}
		idx[pb.Package] = len(out)
		out = append(out, pb)
	}
	return out
}
