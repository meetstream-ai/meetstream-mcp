// Package tools registers the 19 MeetStream MCP tools.
//
// The tool definitions (names, titles, descriptions, input schemas, annotations)
// are embedded verbatim from the Node server's tools/list output, so clients see
// the exact same contract. Handlers are ported from src/server.js.
//
// The server is built once at startup and shared by every request; each call
// reads its MeetStream API key from the request's TokenInfo (set by the HTTP auth
// layer, or by the stdio entry point). Nothing is rebuilt per request.
package tools

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/meetstream-ai/meetstream-mcp/go/internal/meetstream"
	"github.com/meetstream-ai/meetstream-mcp/go/internal/ordered"
)

//go:embed tools.json
var toolsJSON []byte

//go:embed webhook_guide.txt
var webhookGuide string

// APIKeyExtra is the TokenInfo.Extra key holding the caller's MeetStream API key.
const APIKeyExtra = "meetstream_api_key"

// Config controls the API client used by every tool call.
type Config struct {
	BaseURL string // "" = production
	// FallbackKey is used when a request carries no key (stdio mode, or a
	// single-tenant deployment). The hosted server leaves it empty.
	FallbackKey string
	// OnCall is invoked after every tool call (telemetry). May be nil.
	OnCall func(tool string, ok bool)
}

type toolDef struct {
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations json.RawMessage `json:"annotations"`
}

type handler func(ctx context.Context, c *meetstream.Client, args json.RawMessage) (any, error)

// Register adds every tool to srv. It panics if tools.json and the handler
// table disagree, so a mismatch fails at startup rather than at call time.
func Register(srv *mcp.Server, cfg Config) {
	var defs []toolDef
	if err := json.Unmarshal(toolsJSON, &defs); err != nil {
		panic(fmt.Errorf("tools.json: %w", err))
	}
	hs := handlers()
	// Node answers a call to an unknown tool with an isError result rather than a
	// JSON-RPC error; keep that so clients see the same thing.
	srv.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if ctr, ok := req.(*mcp.CallToolRequest); ok && method == "tools/call" && ctr.Params != nil {
				if _, known := hs[ctr.Params.Name]; !known {
					return textResult(fmt.Sprintf("MCP error -32602: Tool %s not found", ctr.Params.Name), true), nil
				}
			}
			return next(ctx, method, req)
		}
	})
	if len(defs) != len(hs) {
		panic(fmt.Errorf("tools.json has %d tools, handler table has %d", len(defs), len(hs)))
	}
	for _, d := range defs {
		h, ok := hs[d.Name]
		if !ok {
			panic(fmt.Errorf("no handler for tool %q", d.Name))
		}
		var ann mcp.ToolAnnotations
		if len(d.Annotations) > 0 {
			if err := json.Unmarshal(d.Annotations, &ann); err != nil {
				panic(fmt.Errorf("tool %q annotations: %w", d.Name, err))
			}
		}
		validator := mustValidator(d.Name, d.InputSchema)
		name := d.Name
		srv.AddTool(&mcp.Tool{
			Name:        d.Name,
			Title:       d.Title,
			Description: d.Description,
			InputSchema: d.InputSchema,
			Annotations: &ann,
		}, func(ctx context.Context, req *mcp.CallToolRequest) (res *mcp.CallToolResult, err error) {
			args := req.Params.Arguments
			if len(bytes.TrimSpace(args)) == 0 || string(bytes.TrimSpace(args)) == "null" {
				args = json.RawMessage(`{}`)
			}
			if verr := validator(args); verr != nil {
				return textResult(fmt.Sprintf("MCP error -32602: Input validation error: Invalid arguments for tool %s: %s", name, verr), true), nil
			}
			// Counted after validation, as in Node; ok means the call did not end in an error result.
			defer func() {
				if cfg.OnCall != nil {
					cfg.OnCall(name, err == nil && res != nil && !res.IsError)
				}
			}()
			key := apiKey(req, cfg.FallbackKey)
			// The webhook guide needs no API call (Node never built a client for it).
			if key == "" && name != "webhook_events_guide" {
				return textResult("MeetStream API error: MEETSTREAM_API_KEY is not set. Create a key at https://app.meetstream.ai/api-keys and set it in this server's environment.", true), nil
			}
			out, herr := h(ctx, meetstream.New(key, cfg.BaseURL), args)
			if herr != nil {
				return textResult("MeetStream API error: "+herr.Error(), true), nil
			}
			return textResult(render(out), false), nil
		})
	}
}

func apiKey(req *mcp.CallToolRequest, fallback string) string {
	if req.Extra != nil && req.Extra.TokenInfo != nil {
		if k, _ := req.Extra.TokenInfo.Extra[APIKeyExtra].(string); k != "" {
			return k
		}
	}
	return fallback
}

func textResult(text string, isErr bool) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}, IsError: isErr}
}

// render matches Node's json(): strings pass through, everything else is
// pretty-printed JSON with two-space indentation.
func render(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case *meetstream.Response:
		return renderResponse(t)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func renderResponse(r *meetstream.Response) string {
	if !r.IsJSON {
		if r.Text != "" {
			return r.Text
		}
		return "null"
	}
	if len(r.Raw) > 0 && r.Raw[0] == '"' {
		var s string
		if json.Unmarshal(r.Raw, &s) == nil {
			return s
		}
	}
	var b bytes.Buffer
	if json.Indent(&b, r.Raw, "", "  ") != nil {
		return string(r.Raw)
	}
	return b.String()
}

func mustValidator(name string, schema json.RawMessage) func(json.RawMessage) error {
	var m map[string]any
	if err := json.Unmarshal(schema, &m); err != nil {
		panic(fmt.Errorf("tool %q schema: %w", name, err))
	}
	delete(m, "$schema") // the Node server emits a draft-07 marker; the rules themselves are dialect-neutral
	// The advertised schema says additionalProperties:false, but Node (zod) strips
	// unknown keys and carries on. Clients that send an extra field must keep working.
	delete(m, "additionalProperties")
	b, _ := json.Marshal(m)
	var s jsonschema.Schema
	if err := json.Unmarshal(b, &s); err != nil {
		panic(fmt.Errorf("tool %q schema: %w", name, err))
	}
	rs, err := s.Resolve(nil)
	if err != nil {
		panic(fmt.Errorf("tool %q schema: %w", name, err))
	}
	return func(args json.RawMessage) error {
		var inst any
		if err := json.Unmarshal(args, &inst); err != nil {
			return err
		}
		if err := rs.Validate(inst); err != nil {
			return cleanValidationError(err)
		}
		return nil
	}
}

// cleanValidationError drops jsonschema-go's "validating root: validating
// /properties/x:" scaffolding so the model sees "x: ..." like the Node errors.
func cleanValidationError(err error) error {
	msg := strings.TrimPrefix(err.Error(), "validating root: ")
	if strings.HasPrefix(msg, "validating /properties/") {
		rest := strings.TrimPrefix(msg, "validating /properties/")
		if i := strings.Index(rest, ": "); i > 0 {
			msg = rest[:i] + ": " + strings.TrimPrefix(rest[i+2:], strings.SplitN(rest[i+2:], ": ", 2)[0]+": ")
		}
	}
	return errors.New(msg)
}

func decode[T any](args json.RawMessage) (T, error) {
	var v T
	err := json.Unmarshal(args, &v)
	return v, err
}

type botArg struct {
	BotID string `json:"bot_id"`
}

func simple(fn func(*meetstream.Client, context.Context, string) (*meetstream.Response, error)) handler {
	return func(ctx context.Context, c *meetstream.Client, args json.RawMessage) (any, error) {
		a, err := decode[botArg](args)
		if err != nil {
			return nil, err
		}
		return fn(c, ctx, a.BotID)
	}
}

func handlers() map[string]handler {
	return map[string]handler{
		// ── Bot lifecycle ─────────────────────────────────────────────
		"create_bot": func(ctx context.Context, c *meetstream.Client, args json.RawMessage) (any, error) {
			var a struct {
				MeetingLink              string          `json:"meeting_link"`
				BotName                  string          `json:"bot_name"`
				RecordVideo              bool            `json:"record_video"`
				VideoLayout              string          `json:"video_layout"`
				TranscriptionProvider    string          `json:"transcription_provider"`
				Language                 string          `json:"language"`
				CallbackURL              string          `json:"callback_url"`
				JoinAt                   string          `json:"join_at"`
				BotMessage               string          `json:"bot_message"`
				BotImageURL              string          `json:"bot_image_url"`
				RetentionHours           float64         `json:"retention_hours"`
				SeparateAudioStreams     bool            `json:"separate_audio_streams"`
				SeparateVideoStreams     bool            `json:"separate_video_streams"`
				AgentConfigID            string          `json:"agent_config_id"`
				LiveTranscriptWebhookURL string          `json:"live_transcript_webhook_url"`
				CustomAttributes         json.RawMessage `json:"custom_attributes"`
				IdempotencyKey           string          `json:"idempotency_key"`
				GoogleLoginDomain        string          `json:"google_login_domain"`
				TeamsLoginDomain         string          `json:"teams_login_domain"`
				SignInEmail              string          `json:"sign_in_email"`
				StrictEmail              *bool           `json:"strict_email"`
				ZoomZakURL               string          `json:"zoom_zak_url"`
				ZoomObfURL               string          `json:"zoom_obf_url"`
			}
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, err
			}
			payload, err := meetstream.BuildCreateBotPayload(meetstream.BotOptions{
				MeetingLink: a.MeetingLink, Name: a.BotName, Video: a.RecordVideo, VideoLayout: a.VideoLayout,
				Transcript: a.TranscriptionProvider, Language: a.Language, Callback: a.CallbackURL,
				JoinAt: a.JoinAt, BotMessage: a.BotMessage, ImageURL: a.BotImageURL,
				RetentionHours: a.RetentionHours, SeparateAudio: a.SeparateAudioStreams,
				SeparateVideo: a.SeparateVideoStreams, AgentConfigID: a.AgentConfigID,
				LiveTranscriptWebhook: a.LiveTranscriptWebhookURL, Attrs: a.CustomAttributes,
				GoogleLoginDomain: a.GoogleLoginDomain, TeamsLoginDomain: a.TeamsLoginDomain,
				SignInEmail: a.SignInEmail, StrictEmail: a.StrictEmail,
				ZoomZakURL: a.ZoomZakURL, ZoomObfURL: a.ZoomObfURL,
			})
			if err != nil {
				return nil, err
			}
			r, err := c.CreateBot(ctx, payload, a.IdempotencyKey)
			if err != nil {
				return nil, err
			}
			// { ...data, idempotent_replay: status === 507 || undefined, sent_payload: payload }
			out, ok := ordered.FromRaw(r.Raw)
			if !ok {
				out = ordered.New()
			}
			if r.Status == 507 {
				out.Set("idempotent_replay", true)
			}
			out.Set("sent_payload", payload)
			return out, nil
		},
		"list_bots": func(ctx context.Context, c *meetstream.Client, _ json.RawMessage) (any, error) {
			return c.ListBots(ctx)
		},
		"get_bot_status":  simple((*meetstream.Client).BotStatus),
		"get_bot_detail":  simple((*meetstream.Client).BotDetail),
		"get_bot_summary": simple((*meetstream.Client).BotSummary),
		"remove_bot":      simple((*meetstream.Client).RemoveBot),
		"delete_bot_data": simple((*meetstream.Client).DeleteBotData), // schema requires confirm: true

		// ── Transcripts ───────────────────────────────────────────────
		"get_transcript": func(ctx context.Context, c *meetstream.Client, args json.RawMessage) (any, error) {
			var a struct {
				BotID          string `json:"bot_id"`
				Wait           bool   `json:"wait"`
				TimeoutSeconds *int64 `json:"timeout_seconds"`
				Raw            bool   `json:"raw"`
			}
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, err
			}
			timeout := int64(300)
			if a.TimeoutSeconds != nil {
				timeout = *a.TimeoutSeconds
			}
			// Unbounded in the schema; cap it so one call cannot hold a connection
			// for hours (the HTTP write timeout and nginx are sized for 10 minutes).
			if timeout > 600 {
				timeout = 600
			}
			r, err := c.GetTranscript(ctx, a.BotID, a.Raw, a.Wait, time.Duration(timeout)*time.Second)
			if err != nil {
				return nil, err
			}
			var tid any = r.TranscriptID
			if r.TranscriptID == "" {
				tid = nil
			}
			if r.Segments == nil {
				return ordered.New().Set("ready", false).Set("transcript_id", tid).
					Set("hint", "Transcript not ready yet. Wait for the transcription.processed webhook or call again with wait=true. Streaming-only providers never produce a post-call transcript."), nil
			}
			return ordered.New().Set("ready", true).Set("transcript_id", tid).Set("segments", r.Segments), nil
		},
		"list_transcriptions": simple((*meetstream.Client).Transcriptions),
		"transcribe_audio": func(ctx context.Context, c *meetstream.Client, args json.RawMessage) (any, error) {
			var a struct {
				BotID       string `json:"bot_id"`
				Provider    string `json:"provider"`
				Language    string `json:"language"`
				CallbackURL string `json:"callback_url"`
			}
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, err
			}
			return c.Transcribe(ctx, a.BotID, meetstream.TranscribeProvider(a.Provider, a.Language), a.CallbackURL)
		},

		// ── Media + meeting data ──────────────────────────────────────
		"get_media_urls": func(ctx context.Context, c *meetstream.Client, args json.RawMessage) (any, error) {
			var a struct {
				BotID string `json:"bot_id"`
				Kind  string `json:"kind"`
			}
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, err
			}
			switch a.Kind {
			case "", "audio":
				return c.BotAudio(ctx, a.BotID)
			case "video":
				return c.BotVideo(ctx, a.BotID)
			case "audio_streams":
				return c.AudioStreams(ctx, a.BotID)
			case "video_streams":
				return c.RecordingStreams(ctx, a.BotID)
			case "screenshots":
				return c.Screenshots(ctx, a.BotID)
			}
			return nil, errors.New("unknown kind " + a.Kind)
		},
		"get_participants":     simple((*meetstream.Client).Participants),
		"get_chats":            simple((*meetstream.Client).Chats),
		"get_speaker_timeline": simple((*meetstream.Client).SpeakerTimeline),

		// ── Live-meeting interaction ──────────────────────────────────
		"send_chat_message": func(ctx context.Context, c *meetstream.Client, args json.RawMessage) (any, error) {
			var a struct {
				BotID   string `json:"bot_id"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, err
			}
			return c.SendMessage(ctx, a.BotID, a.Message)
		},
		"send_image": func(ctx context.Context, c *meetstream.Client, args json.RawMessage) (any, error) {
			var a struct {
				BotID                  string `json:"bot_id"`
				ImgURL                 string `json:"img_url"`
				DisplayDurationSeconds int64  `json:"display_duration_seconds"`
			}
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, err
			}
			return c.SendImage(ctx, a.BotID, a.ImgURL, a.DisplayDurationSeconds)
		},

		// ── Calendar ──────────────────────────────────────────────────
		"list_calendar_events": func(ctx context.Context, c *meetstream.Client, _ json.RawMessage) (any, error) {
			return c.CalendarEvents(ctx)
		},
		"schedule_calendar_bot": func(ctx context.Context, c *meetstream.Client, args json.RawMessage) (any, error) {
			var a struct {
				EventID string `json:"event_id"`
				Action  string `json:"action"`
			}
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, err
			}
			if strings.EqualFold(a.Action, "unschedule") {
				return c.UnscheduleEvent(ctx, a.EventID)
			}
			return c.ScheduleEvent(ctx, a.EventID)
		},

		// ── Reference (needs no API call, but keeps the same auth gate) ─
		"webhook_events_guide": func(context.Context, *meetstream.Client, json.RawMessage) (any, error) {
			return webhookGuide, nil
		},
	}
}
