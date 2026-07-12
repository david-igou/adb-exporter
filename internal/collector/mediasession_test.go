package collector

import "testing"

// mediaIdleSample is the real idle-session output from the reference device
// (SPEC.md §6.8): one Netflix session with state=null.
const mediaIdleSample = `  Sessions Stack - have 1 sessions:
    Netflix media session com.netflix.ninja/Netflix media session (userId=0)
      ownerPid=5143, ownerUid=10088, userId=0
      package=com.netflix.ninja
      active=false
      state=null`

// mediaPlayingSample models an active PlaybackState (state=3 PLAYING), the form
// the spec says renders only while media is active.
const mediaPlayingSample = `  Sessions Stack - have 2 sessions:
    Netflix media session com.netflix.ninja/Netflix media session (userId=0)
      package=com.netflix.ninja
      active=true
      state=PlaybackState {state=3, position=1234, buffered position=0, speed=1.0, updated=999}
    Some other session com.example.player/session (userId=0)
      package=com.example.player
      state=null`

func TestParseMediaSessionIdle(t *testing.T) {
	got := parseMediaSession(mediaIdleSample)
	if got.Count != 1 {
		t.Errorf("count = %d, want 1", got.Count)
	}
	if len(got.Playbacks) != 0 {
		t.Errorf("playbacks = %+v, want none (idle)", got.Playbacks)
	}
}

func TestParseMediaSessionPlaying(t *testing.T) {
	got := parseMediaSession(mediaPlayingSample)
	if got.Count != 2 {
		t.Errorf("count = %d, want 2", got.Count)
	}
	if len(got.Playbacks) != 1 {
		t.Fatalf("playbacks = %+v, want 1", got.Playbacks)
	}
	if got.Playbacks[0] != (playbackState{Package: "com.netflix.ninja", State: 3}) {
		t.Errorf("playback = %+v", got.Playbacks[0])
	}
}

func TestParseMediaSessionCountFallback(t *testing.T) {
	// No "Sessions Stack" phrase ⇒ count package= lines.
	in := `      package=com.a
      state=null
      package=com.b
      state=null`
	got := parseMediaSession(in)
	if got.Count != 2 {
		t.Errorf("fallback count = %d, want 2", got.Count)
	}
}
