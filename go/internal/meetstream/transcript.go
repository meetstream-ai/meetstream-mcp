package meetstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"time"
)

var doneStatus = regexp.MustCompile(`(?i)success|completed`)

// ResolveTranscriptID finds a bot's transcript_id. It is NOT delivered in
// webhooks; the canonical sources are /detail (bot_details.transcript_id) and
// /transcriptions.
func (c *Client) ResolveTranscriptID(ctx context.Context, botID string) (string, error) {
	if r, err := c.BotDetail(ctx, botID); err == nil && r.IsJSON {
		var d struct {
			BotDetails struct {
				TranscriptID string `json:"transcript_id"`
			} `json:"bot_details"`
			TranscriptID string `json:"transcript_id"`
		}
		if json.Unmarshal(r.Raw, &d) == nil {
			if d.BotDetails.TranscriptID != "" {
				return d.BotDetails.TranscriptID, nil
			}
			if d.TranscriptID != "" {
				return d.TranscriptID, nil
			}
		}
	}
	r, err := c.Transcriptions(ctx, botID)
	if err != nil {
		return "", err
	}
	var d struct {
		Transcriptions []struct {
			TranscriptID string `json:"transcript_id"`
			Status       string `json:"status"`
		} `json:"transcriptions"`
	}
	if r.IsJSON {
		_ = json.Unmarshal(r.Raw, &d)
	}
	for _, t := range d.Transcriptions {
		if doneStatus.MatchString(t.Status) {
			return t.TranscriptID, nil
		}
	}
	if len(d.Transcriptions) > 0 {
		return d.Transcriptions[0].TranscriptID, nil
	}
	return "", nil
}

// TranscriptResult is what GetTranscript found. Segments is nil when not ready.
type TranscriptResult struct {
	TranscriptID string
	Segments     json.RawMessage
}

// GetTranscript fetches a bot's transcript, optionally polling until it is ready.
//
// One deliberate difference from the Node server: an HTTP 202 ("still processing")
// counts as not ready. Node treated the 202 body as the transcript, so a
// streaming-only bot reported ready=true with no segments.
func (c *Client) GetTranscript(ctx context.Context, botID string, raw, wait bool, timeout time.Duration) (*TranscriptResult, error) {
	deadline := time.Now().Add(timeout)
	for {
		tid, err := c.ResolveTranscriptID(ctx, botID)
		if err != nil {
			return nil, err
		}
		if tid != "" {
			r, err := c.TranscriptByID(ctx, tid, raw)
			if err != nil {
				var ae *APIError
				if !wait || (errors.As(err, &ae) && ae.Status != 404 && ae.Status != 400) {
					return nil, err
				}
			} else if r.Status != 202 {
				if seg := segments(r); seg != nil {
					return &TranscriptResult{TranscriptID: tid, Segments: seg}, nil
				}
			}
		}
		if !wait {
			return &TranscriptResult{TranscriptID: tid}, nil
		}
		if time.Now().After(deadline) {
			return nil, &APIError{
				Message: fmt.Sprintf("Timed out after %ds waiting for transcript of bot %s", int(math.Round(timeout.Seconds())), botID),
				Path:    "/bots/" + botID,
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// segments unwraps the transcript body. The live API may wrap segments in a
// `message` key: { "message": [ {speaker, transcript, ...} ] }. An empty list is
// not ready.
func segments(r *Response) json.RawMessage {
	if !r.IsJSON {
		return nil
	}
	var probe any
	if json.Unmarshal(r.Raw, &probe) != nil || probe == nil {
		return nil
	}
	switch t := probe.(type) {
	case []any:
		if len(t) == 0 {
			return nil
		}
		return r.Raw
	case map[string]any:
		if inner, ok := t["message"].([]any); ok {
			if len(inner) == 0 {
				return nil
			}
			var wrap struct {
				Message json.RawMessage `json:"message"`
			}
			_ = json.Unmarshal(r.Raw, &wrap)
			return wrap.Message
		}
		return r.Raw
	default:
		return r.Raw
	}
}
