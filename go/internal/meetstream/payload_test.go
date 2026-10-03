package meetstream

import (
	"encoding/json"
	"regexp"
	"testing"
)

func build(t *testing.T, o BotOptions) map[string]any {
	t.Helper()
	p, err := BuildCreateBotPayload(o)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	b, _ := json.Marshal(p)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func mustFail(t *testing.T, o BotOptions, pattern string) {
	t.Helper()
	_, err := BuildCreateBotPayload(o)
	if err == nil || !regexp.MustCompile(pattern).MatchString(err.Error()) {
		t.Fatalf("want error matching %q, got %v", pattern, err)
	}
}

func eq(t *testing.T, got any, want string) {
	t.Helper()
	b, _ := json.Marshal(got)
	if string(b) != want {
		t.Fatalf("got %s, want %s", b, want)
	}
}

func TestTeamsSignedIn(t *testing.T) {
	f := false
	p := build(t, BotOptions{MeetingLink: "https://teams.microsoft.com/l/x", TeamsLoginDomain: "bots.acme.com", SignInEmail: "bot1@bots.acme.com", StrictEmail: &f})
	eq(t, p["teams"], `{"login_required":true,"sign_in_email":"bot1@bots.acme.com","strict_email":false,"teams_login_domain":"bots.acme.com"}`)
	if _, ok := p["google_meet"]; ok {
		t.Fatal("unexpected google_meet")
	}
}

func TestGoogleSignedIn(t *testing.T) {
	p := build(t, BotOptions{MeetingLink: "https://meet.google.com/x", GoogleLoginDomain: "acme.com"})
	eq(t, p["google_meet"], `{"google_login_domain":"acme.com","login_required":true}`)
}

func TestInvalidCombinations(t *testing.T) {
	mustFail(t, BotOptions{MeetingLink: "m", GoogleLoginDomain: "a", TeamsLoginDomain: "b"}, "only one")
	mustFail(t, BotOptions{MeetingLink: "m", SignInEmail: "x@y"}, "need google_login_domain or teams_login_domain")
	mustFail(t, BotOptions{MeetingLink: "m", ZoomZakURL: "https://a", ZoomObfURL: "https://b"}, "only one")
}

func TestZoomTokens(t *testing.T) {
	eq(t, build(t, BotOptions{MeetingLink: "https://zoom.us/j/1", ZoomZakURL: "https://x/zak"})["zoom"], `{"zak_url":"https://x/zak"}`)
	eq(t, build(t, BotOptions{MeetingLink: "https://zoom.us/j/1", ZoomObfURL: "https://x/obf"})["zoom"], `{"obf_url":"https://x/obf"}`)
	if _, ok := build(t, BotOptions{MeetingLink: "https://zoom.us/j/1"})["zoom"]; ok {
		t.Fatal("unexpected zoom block")
	}
}

func TestVideoDefaults(t *testing.T) {
	audio := build(t, BotOptions{MeetingLink: "https://meet.google.com/x"})
	if audio["video_required"] != false {
		t.Fatal("video must be off by default")
	}
	if rc, ok := audio["recording_config"].(map[string]any); ok && rc["video_layout"] != nil {
		t.Fatal("no layout without video")
	}
	video := build(t, BotOptions{MeetingLink: "https://meet.google.com/x", Video: true})
	if video["video_required"] != true || video["recording_config"].(map[string]any)["video_layout"] != "speaker_view" {
		t.Fatalf("want speaker_view, got %v", video["recording_config"])
	}
	if _, ok := video["video_separate_streams"]; ok {
		t.Fatal("per-participant video must stay opt-in")
	}
	grid := build(t, BotOptions{MeetingLink: "m", Video: true, VideoLayout: "grid_view"})
	if grid["recording_config"].(map[string]any)["video_layout"] != "grid_view" {
		t.Fatal("grid_view not honoured")
	}
}

func TestVideoLayoutValidation(t *testing.T) {
	mustFail(t, BotOptions{MeetingLink: "m", VideoLayout: "speaker_view"}, "needs record_video")
	mustFail(t, BotOptions{MeetingLink: "m", Video: true, VideoLayout: "gallery"}, "speaker_view.*grid_view")
}

func TestKeyOrderMatchesNode(t *testing.T) {
	p, _ := BuildCreateBotPayload(BotOptions{MeetingLink: "m", Callback: "https://cb", Video: true})
	b, _ := json.Marshal(p)
	want := regexp.MustCompile(`^\{"meeting_link":"m","bot_name":"MeetStream Bot","video_required":true,"callback_url":"https://cb",.*"automatic_leave":`)
	if !want.Match(b) {
		t.Fatalf("key order: %s", b)
	}
}
