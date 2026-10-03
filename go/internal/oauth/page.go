package oauth

import (
	"html/template"
	"net/http"
)

type pageData struct {
	Title        string
	Client       string
	RedirectHost string
	Request      string
	APIKeysURL   string
	Error        string
	Form         bool
}

var pageTmpl = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>{{.Title}} · MeetStream</title>
<style>
:root{--bg:#f7f7f5;--card:#fff;--fg:#17171a;--muted:#5d5d66;--line:#e3e3e0;--accent:#ff5a1f;--accent-fg:#fff;--err:#b42318;--errbg:#fef3f2}
@media (prefers-color-scheme:dark){:root{--bg:#121214;--card:#1b1b1f;--fg:#ededef;--muted:#a1a1aa;--line:#2c2c33;--accent:#ff7a45;--accent-fg:#141414;--err:#fda29b;--errbg:#2a1715}}
*{box-sizing:border-box}
body{margin:0;min-height:100vh;display:grid;place-items:center;background:var(--bg);color:var(--fg);font:16px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Inter,Roboto,sans-serif;padding:24px 16px}
main{width:100%;max-width:440px;background:var(--card);border:1px solid var(--line);border-radius:14px;padding:28px}
.brand{font-weight:700;letter-spacing:-.01em;margin-bottom:18px}
.brand span{color:var(--accent)}
h1{font-size:20px;line-height:1.3;margin:0 0 8px}
p{margin:0 0 14px;color:var(--muted)}
p strong{color:var(--fg)}
ol{margin:0 0 18px;padding-left:20px;color:var(--muted)}
ol li{margin:4px 0}
a{color:var(--accent)}
label{display:block;font-weight:600;margin:0 0 6px}
input[type=password]{width:100%;padding:11px 12px;border:1px solid var(--line);border-radius:9px;background:transparent;color:var(--fg);font:15px ui-monospace,SFMono-Regular,Menlo,monospace}
input[type=password]:focus{outline:2px solid var(--accent);outline-offset:1px;border-color:transparent}
.actions{display:flex;gap:10px;margin-top:18px}
button{flex:1;padding:11px 14px;border-radius:9px;border:1px solid var(--line);font:600 15px inherit;cursor:pointer;background:transparent;color:var(--fg)}
button.primary{background:var(--accent);color:var(--accent-fg);border-color:var(--accent)}
.err{background:var(--errbg);color:var(--err);border-radius:9px;padding:10px 12px;margin:0 0 16px;font-size:14px}
.fine{font-size:13px;margin-top:16px;margin-bottom:0}
</style>
</head>
<body>
<main>
<div class="brand">Meet<span>Stream</span></div>
<h1>{{.Title}}</h1>
{{if .Error}}<div class="err" role="alert">{{.Error}}</div>{{end}}
{{if .Form}}
<p><strong>{{.Client}}</strong>{{if .RedirectHost}} ({{.RedirectHost}}){{end}} wants to use your MeetStream account to create and manage meeting bots, transcripts and recordings.</p>
<ol>
<li><a href="{{.APIKeysURL}}" target="_blank" rel="noopener">Open your MeetStream API keys</a> and create or copy a key.</li>
<li>Paste it below and select Connect.</li>
</ol>
<form method="post" action="/oauth/authorize" autocomplete="off">
<input type="hidden" name="request" value="{{.Request}}">
<label for="api_key">MeetStream API key</label>
<input type="password" id="api_key" name="api_key" placeholder="ms_..." spellcheck="false" autocapitalize="off" autofocus>
<div class="actions">
<button type="submit" name="action" value="deny">Cancel</button>
<button type="submit" name="action" value="allow" class="primary">Connect</button>
</div>
</form>
<p class="fine">The key travels encrypted inside the token issued to {{.Client}}; this server keeps no copy. To disconnect later, delete the key in your dashboard.</p>
{{end}}
</main>
</body>
</html>`))

func (s *Server) page(w http.ResponseWriter, status int, d pageData) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'")
	w.WriteHeader(status)
	_ = pageTmpl.Execute(w, d)
}
