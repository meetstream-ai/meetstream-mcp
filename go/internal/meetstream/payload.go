package meetstream

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/meetstream-ai/meetstream-mcp/go/internal/ordered"
)

// BotOptions are the create_bot inputs, named after the Node buildCreateBotPayload options.
// Zero values mean "not provided", matching the JavaScript truthiness checks.
type BotOptions struct {
	MeetingLink           string
	Name                  string
	Video                 bool
	VideoLayout           string
	Transcript            string
	Language              string
	Callback              string
	JoinAt                string
	BotMessage            string
	ImageURL              string
	RetentionHours        float64
	SeparateAudio         bool
	SeparateVideo         bool
	AgentConfigID         string
	LiveTranscriptWebhook string
	Attrs                 json.RawMessage // a JSON object of string values, kept in caller order
	GoogleLoginDomain     string
	TeamsLoginDomain      string
	SignInEmail           string
	StrictEmail           *bool
	ZoomZakURL            string
	ZoomObfURL            string
}

// BuildCreateBotPayload builds the create_bot body with MeetStream's safe defaults:
// audio only unless video was asked for, speaker view when it was, per-participant
// video only on request, and automatic_leave timeouts so a bot never idles.
// It is a line-for-line port of buildCreateBotPayload in src/api.js, including key order.
func BuildCreateBotPayload(o BotOptions) (*ordered.Object, error) {
	name := o.Name
	if name == "" {
		name = "MeetStream Bot"
	}
	p := ordered.New().
		Set("meeting_link", o.MeetingLink).
		Set("bot_name", name).
		Set("video_required", o.Video)

	if o.BotMessage != "" {
		p.Set("bot_message", o.BotMessage)
	}
	if o.ImageURL != "" {
		p.Set("bot_image_url", o.ImageURL) // must be a PUBLIC url
	}
	if o.Callback != "" {
		p.Set("callback_url", o.Callback)
	}
	if o.JoinAt != "" {
		p.Set("join_at", o.JoinAt)
	}
	if o.AgentConfigID != "" {
		p.Set("agent_config_id", o.AgentConfigID)
	}
	if o.SeparateAudio {
		p.Set("audio_separate_streams", true)
	}
	// Per-participant video is opt-in only: never set implicitly.
	if o.SeparateVideo {
		p.Set("video_separate_streams", true)
	}
	if o.LiveTranscriptWebhook != "" {
		p.Set("live_transcription_required", ordered.New().Set("webhook_url", o.LiveTranscriptWebhook))
	}
	if attrs, ok := ordered.FromRaw(o.Attrs); ok && attrs.Len() > 0 {
		p.Set("custom_attributes", attrs)
	}

	// Signed-in joins. The domain must already be registered on the account
	// (google-login-domains / teams-login-domains); the API returns 400 otherwise.
	if o.GoogleLoginDomain != "" && o.TeamsLoginDomain != "" {
		return nil, errors.New("Pass only one of google_login_domain or teams_login_domain.")
	}
	if (o.SignInEmail != "" || o.StrictEmail != nil) && o.GoogleLoginDomain == "" && o.TeamsLoginDomain == "" {
		return nil, errors.New("sign_in_email and strict_email need google_login_domain or teams_login_domain.")
	}
	signIn := func(domainKey, domain string) *ordered.Object {
		s := ordered.New().Set("login_required", true).Set(domainKey, domain)
		if o.SignInEmail != "" {
			s.Set("sign_in_email", o.SignInEmail)
		}
		if o.StrictEmail != nil {
			s.Set("strict_email", *o.StrictEmail)
		}
		return s
	}
	if o.GoogleLoginDomain != "" {
		p.Set("google_meet", signIn("google_login_domain", o.GoogleLoginDomain))
	}
	if o.TeamsLoginDomain != "" {
		p.Set("teams", signIn("teams_login_domain", o.TeamsLoginDomain))
	}

	// Authenticated Zoom joins: HTTPS endpoints on the caller's server that return a fresh token.
	if o.ZoomZakURL != "" && o.ZoomObfURL != "" {
		return nil, errors.New("Pass only one of zoom_zak_url or zoom_obf_url.")
	}
	if o.ZoomZakURL != "" {
		p.Set("zoom", ordered.New().Set("zak_url", o.ZoomZakURL))
	}
	if o.ZoomObfURL != "" {
		p.Set("zoom", ordered.New().Set("obf_url", o.ZoomObfURL))
	}

	var rc *ordered.Object
	recordingConfig := func() *ordered.Object {
		if rc == nil {
			rc = ordered.New()
			p.Set("recording_config", rc)
		}
		return rc
	}
	if o.Transcript != "" {
		recordingConfig().Set("transcript", ordered.New().Set("provider", transcriptProvider(o.Transcript, o.Language)))
	}
	if o.RetentionHours != 0 {
		recordingConfig().Set("retention", ordered.New().Set("type", "timed").Set("hours", o.RetentionHours))
	}

	// Video defaults: audio only unless video was asked for, and when it was,
	// speaker view unless grid was asked for (the API itself defaults to grid_view).
	// video_layout only applies to mixed video, so it is skipped for audio-only bots.
	if o.Video {
		layout := strings.ToLower(o.VideoLayout)
		if layout == "" {
			layout = "speaker_view"
		}
		if layout != "speaker_view" && layout != "grid_view" {
			return nil, errors.New("video_layout must be 'speaker_view' or 'grid_view'.")
		}
		recordingConfig().Set("video_layout", layout)
	} else if o.VideoLayout != "" {
		return nil, errors.New("video_layout needs record_video: true (an audio-only bot records no mixed video).")
	}

	// Sensible timeouts so bots never sit in empty meetings.
	// recording_permission_denied_timeout: Zoom-only, MINIMUM 60 (lower → HTTP 400).
	p.Set("automatic_leave", ordered.New().
		Set("waiting_room_timeout", 300).
		Set("everyone_left_timeout", 60).
		Set("in_call_recording_timeout", 14400).
		Set("recording_permission_denied_timeout", 60))
	return p, nil
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func transcriptProvider(name, lang string) *ordered.Object {
	pr := ordered.New()
	switch name {
	case "deepgram":
		pr.Set("deepgram", ordered.New().Set("model", "nova-3").Set("language", or(lang, "en")).Set("diarize", true))
	case "assemblyai":
		pr.Set("assemblyai", ordered.New().Set("speech_models", []string{"best"}).Set("language_code", or(lang, "en_us")).Set("speaker_labels", true))
	case "sarvam":
		pr.Set("sarvam", ordered.New().Set("model", "saarika:v2").Set("language_code", or(lang, "en-IN")).Set("mode", "batch").Set("with_diarization", true))
	case "meetstream":
		pr.Set("meetstream", ordered.New().Set("language", or(lang, "auto")).Set("translate", false))
	case "jigsawstack":
		pr.Set("jigsawstack", ordered.New().Set("language", or(lang, "auto")).Set("translate", false).Set("by_speaker", true))
	case "deepgram_streaming":
		pr.Set("deepgram_streaming", ordered.New().Set("model", "nova-3").Set("language", or(lang, "en")))
	default: // meeting_captions, assemblyai_streaming
		pr.Set(name, ordered.New())
	}
	return pr
}

// TranscribeProvider builds the provider block for POST /bots/{id}/transcribe.
func TranscribeProvider(provider, lang string) *ordered.Object {
	pr := ordered.New()
	switch provider {
	case "assemblyai":
		pr.Set("assemblyai", ordered.New().Set("speech_models", []string{"best"}).Set("language_code", or(lang, "en_us")))
	case "sarvam":
		pr.Set("sarvam", ordered.New().Set("model", "saarika:v2").Set("language_code", or(lang, "en-IN")).Set("mode", "batch"))
	case "meetstream":
		pr.Set("meetstream", ordered.New().Set("language", or(lang, "auto")).Set("translate", false))
	case "jigsawstack":
		pr.Set("jigsawstack", ordered.New().Set("language", or(lang, "auto")).Set("translate", false))
	default: // deepgram
		pr.Set("deepgram", ordered.New().Set("model", "nova-3").Set("language", or(lang, "en")))
	}
	return pr
}
