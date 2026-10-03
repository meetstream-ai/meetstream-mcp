// Package meetstream is a small client for the MeetStream REST API, ported from
// the Node server's src/api.js. Auth is `Authorization: Token <key>`.
package meetstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL is the production API.
const DefaultBaseURL = "https://api.meetstream.ai/api/v1"

// APIError mirrors the Node ApiError: the message reads
// "<METHOD> <path> → HTTP <status>: <detail>".
type APIError struct {
	Message string
	Status  int
	Path    string
}

func (e *APIError) Error() string { return e.Message }

// Response is a decoded API response. Raw holds the body exactly as received
// when it was JSON; Text holds it when it was not.
type Response struct {
	Status int
	Raw    json.RawMessage // nil when the body was empty or not JSON
	Text   string          // non-JSON body
	IsJSON bool
}

// Client talks to the API on behalf of one API key.
type Client struct {
	APIKey  string
	BaseURL string
	HTTP    *http.Client
}

// sharedHTTP is reused by every Client: one connection pool for the process.
var sharedHTTP = &http.Client{Timeout: 120 * time.Second}

// New returns a client for apiKey. baseURL "" means DefaultBaseURL.
func New(apiKey, baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{APIKey: apiKey, BaseURL: strings.TrimRight(baseURL, "/"), HTTP: sharedHTTP}
}

// Request performs one API call. A 507 (idempotent replay) is a success.
func (c *Client) Request(ctx context.Context, method, path string, body any, headers map[string]string, query url.Values) (*Response, error) {
	u := c.BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "meetstream-mcp-go")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}
	r := &Response{Status: res.StatusCode}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 {
		if json.Valid(trimmed) {
			r.Raw, r.IsJSON = json.RawMessage(trimmed), true
		} else {
			r.Text = string(data)
		}
	}
	ok := res.StatusCode >= 200 && res.StatusCode < 300
	if !ok && res.StatusCode != 507 {
		return nil, &APIError{
			Message: fmt.Sprintf("%s %s → HTTP %d: %s", method, path, res.StatusCode, errorDetail(r)),
			Status:  res.StatusCode,
			Path:    path,
		}
	}
	return r, nil
}

// errorDetail follows the Node logic: detail || message || error || the body.
func errorDetail(r *Response) string {
	if !r.IsJSON {
		if r.Raw == nil && r.Text == "" {
			return "null"
		}
		return truncate(r.Text, 300)
	}
	var v any
	_ = json.Unmarshal(r.Raw, &v)
	switch t := v.(type) {
	case map[string]any:
		for _, k := range []string{"detail", "message", "error"} {
			if s := truthyString(t[k]); s != "" {
				return s
			}
		}
		return string(r.Raw)
	case []any:
		return string(r.Raw)
	case string:
		return truncate(t, 300)
	default:
		return truncate(string(r.Raw), 300)
	}
}

func truthyString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return ""
	case float64:
		if t == 0 {
			return ""
		}
		return fmt.Sprint(t)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func p(id string) string { return url.PathEscape(id) }

// ── Bots ──────────────────────────────────────────────────────────────────

func (c *Client) CreateBot(ctx context.Context, payload any, idempotencyKey string) (*Response, error) {
	var h map[string]string
	if idempotencyKey != "" {
		h = map[string]string{"Idempotency-Key": idempotencyKey}
	}
	return c.Request(ctx, "POST", "/bots/create_bot", payload, h, nil)
}
func (c *Client) ListBots(ctx context.Context) (*Response, error) {
	return c.Request(ctx, "GET", "/bots", nil, nil, nil)
}
func (c *Client) get(ctx context.Context, path string) (*Response, error) {
	return c.Request(ctx, "GET", path, nil, nil, nil)
}
func (c *Client) BotStatus(ctx context.Context, id string) (*Response, error) {
	return c.get(ctx, "/bots/"+p(id)+"/status")
}
func (c *Client) BotDetail(ctx context.Context, id string) (*Response, error) {
	return c.get(ctx, "/bots/"+p(id)+"/detail")
}
func (c *Client) BotSummary(ctx context.Context, id string) (*Response, error) {
	return c.get(ctx, "/bots/"+p(id)+"/summary")
}
func (c *Client) BotAudio(ctx context.Context, id string) (*Response, error) {
	return c.get(ctx, "/bots/"+p(id)+"/get_audio")
}
func (c *Client) BotVideo(ctx context.Context, id string) (*Response, error) {
	return c.get(ctx, "/bots/"+p(id)+"/get_video")
}
func (c *Client) RecordingStreams(ctx context.Context, id string) (*Response, error) {
	return c.get(ctx, "/bots/"+p(id)+"/get_recording_streams")
}
func (c *Client) AudioStreams(ctx context.Context, id string) (*Response, error) {
	return c.get(ctx, "/bots/"+p(id)+"/get_audio_streams")
}
func (c *Client) SpeakerTimeline(ctx context.Context, id string) (*Response, error) {
	return c.get(ctx, "/bots/"+p(id)+"/get_speaker_timeline")
}
func (c *Client) Chats(ctx context.Context, id string) (*Response, error) {
	return c.get(ctx, "/bots/"+p(id)+"/get_chats")
}
func (c *Client) Screenshots(ctx context.Context, id string) (*Response, error) {
	return c.get(ctx, "/bots/"+p(id)+"/get_screenshots")
}
func (c *Client) Participants(ctx context.Context, id string) (*Response, error) {
	return c.get(ctx, "/bots/"+p(id)+"/get_participants")
}

// RemoveBot is a GET, not a DELETE.
func (c *Client) RemoveBot(ctx context.Context, id string) (*Response, error) {
	return c.get(ctx, "/bots/"+p(id)+"/remove_bot")
}
func (c *Client) DeleteBotData(ctx context.Context, id string) (*Response, error) {
	return c.Request(ctx, "DELETE", "/bots/"+p(id)+"/delete", nil, nil, nil)
}
func (c *Client) SendMessage(ctx context.Context, id, message string) (*Response, error) {
	return c.Request(ctx, "POST", "/bots/"+p(id)+"/send_message", map[string]any{"message": message}, nil, nil)
}
func (c *Client) SendImage(ctx context.Context, id, imgURL string, displayDuration int64) (*Response, error) {
	body := map[string]any{"img_url": imgURL}
	if displayDuration != 0 {
		body["display_duration"] = displayDuration
	}
	return c.Request(ctx, "POST", "/bots/"+p(id)+"/send_image", body, nil, nil)
}

// ── Transcription ─────────────────────────────────────────────────────────

func (c *Client) Transcriptions(ctx context.Context, id string) (*Response, error) {
	return c.get(ctx, "/bots/"+p(id)+"/transcriptions")
}
func (c *Client) Transcribe(ctx context.Context, id string, provider any, callbackURL string) (*Response, error) {
	body := map[string]any{"provider": provider}
	if callbackURL != "" {
		body["callback_url"] = callbackURL
	}
	return c.Request(ctx, "POST", "/bots/"+p(id)+"/transcribe", body, nil, nil)
}
func (c *Client) TranscriptByID(ctx context.Context, tid string, raw bool) (*Response, error) {
	return c.Request(ctx, "GET", "/transcript/"+p(tid)+"/get_transcript", nil, nil, url.Values{"raw": {fmt.Sprint(raw)}})
}

// ── Calendar ──────────────────────────────────────────────────────────────

func (c *Client) CalendarEvents(ctx context.Context) (*Response, error) {
	return c.get(ctx, "/calendar/events")
}
func (c *Client) ScheduleEvent(ctx context.Context, eventID string) (*Response, error) {
	return c.Request(ctx, "POST", "/calendar/schedule/"+p(eventID), nil, nil, nil)
}
func (c *Client) UnscheduleEvent(ctx context.Context, eventID string) (*Response, error) {
	return c.Request(ctx, "DELETE", "/calendar/schedule/"+p(eventID), nil, nil, nil)
}
