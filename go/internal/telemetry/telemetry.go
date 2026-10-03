// Package telemetry sends anonymous, opt-out usage events to PostHog, matching
// src/telemetry.js: only tool names, success flags and counts. Never API keys,
// meeting URLs, transcripts or any content. Disable with DO_NOT_TRACK=1 or
// MEETSTREAM_TELEMETRY=0.
package telemetry

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"os/user"
	"strings"
	"time"
)

const (
	key  = "phc_oFCXdmvQVdgxSwQoCG2GCwupkmt3UqGovj4feMp5ZuCq" // PostHog public project key
	host = "https://us.i.posthog.com"
)

// Client queues events and sends them from one goroutine, so a slow PostHog
// never holds up a tool call.
type Client struct {
	transport string
	id        string
	ch        chan event
	http      *http.Client
}

type event struct {
	name  string
	props map[string]any
}

// Enabled reports whether telemetry is allowed by the environment.
func Enabled() bool {
	switch strings.ToLower(os.Getenv("DO_NOT_TRACK")) {
	case "1", "true":
		return false
	}
	switch strings.ToLower(os.Getenv("MEETSTREAM_TELEMETRY")) {
	case "0", "false", "off":
		return false
	}
	return true
}

// New returns a client, or nil when telemetry is disabled. A nil *Client is
// safe to use.
func New(transport string) *Client {
	if !Enabled() {
		return nil
	}
	c := &Client{transport: transport, id: anonID(), ch: make(chan event, 1024), http: &http.Client{Timeout: 5 * time.Second}}
	go c.loop()
	return c
}

func anonID() string {
	hn, _ := os.Hostname()
	un, home := "", ""
	if u, err := user.Current(); err == nil {
		un, home = u.Username, u.HomeDir
	}
	sum := sha256.Sum256([]byte(hn + "|" + un + "|" + home))
	return "anon_" + hex.EncodeToString(sum[:])[:16]
}

// Track queues an event. It never blocks; events are dropped if the queue is full.
func (c *Client) Track(name string, props map[string]any) {
	if c == nil {
		return
	}
	select {
	case c.ch <- event{name, props}:
	default:
	}
}

func (c *Client) loop() {
	for e := range c.ch {
		props := map[string]any{"transport": c.transport, "$lib": "meetstream-mcp"}
		for k, v := range e.props {
			props[k] = v
		}
		body, _ := json.Marshal(map[string]any{"api_key": key, "event": e.name, "distinct_id": c.id, "properties": props})
		resp, err := c.http.Post(host+"/capture/", "application/json", bytes.NewReader(body))
		if err == nil {
			resp.Body.Close()
		}
	}
}

// Flush waits briefly for queued events to send (used on shutdown).
func (c *Client) Flush(d time.Duration) {
	if c == nil {
		return
	}
	deadline := time.Now().Add(d)
	for len(c.ch) > 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
}
