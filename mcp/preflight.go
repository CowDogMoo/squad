package mcp

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/cowdogmoo/squad/logging"
)

// chromeDevToolsPackage identifies the chrome-devtools-mcp package in a
// server's command/args. Matching is substring-based so it works with
// versioned specifiers like "chrome-devtools-mcp@latest".
const chromeDevToolsPackage = "chrome-devtools-mcp"

// cdpEndpoint is the standard Chrome remote-debugging endpoint
// chrome-devtools-mcp falls back to when Chrome's built-in permission API
// (Chrome 144+) isn't available or hasn't been granted. var so tests can
// redirect the probe at an httptest server.
var cdpEndpoint = "http://127.0.0.1:9222/json/version"

// cdpProbeTimeout caps the pre-flight probe. Fast enough that a missing
// Chrome doesn't measurably delay agent startup; long enough that a loaded
// machine still gets a response.
const cdpProbeTimeout = 750 * time.Millisecond

// isChromeDevToolsMCP reports whether cfg launches chrome-devtools-mcp.
// Detection covers both `npx chrome-devtools-mcp@latest` and direct
// invocations like `chrome-devtools-mcp --autoConnect`.
func isChromeDevToolsMCP(cfg ServerConfig) bool {
	if strings.Contains(cfg.Command, chromeDevToolsPackage) {
		return true
	}
	for _, a := range cfg.Args {
		if strings.Contains(a, chromeDevToolsPackage) {
			return true
		}
	}
	return false
}

// chromeUsesAutoConnect reports whether the server invocation passes
// --autoConnect (with or without an `=value`).
func chromeUsesAutoConnect(cfg ServerConfig) bool {
	for _, a := range cfg.Args {
		if a == "--autoConnect" || strings.HasPrefix(a, "--autoConnect=") {
			return true
		}
	}
	return false
}

// cdpReachable performs a quick HEAD against the standard CDP endpoint.
// A 200 response means Chrome was launched with --remote-debugging-port.
// Any other outcome (timeout, refused, non-2xx) means the legacy file-based
// attach path won't work; the connection then depends entirely on Chrome's
// 144+ permission API being available and granted.
func cdpReachable(ctx context.Context) bool {
	probeCtx, cancel := context.WithTimeout(ctx, cdpProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, cdpEndpoint, nil)
	if err != nil {
		return false
	}
	client := &http.Client{
		Timeout: cdpProbeTimeout,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{Timeout: cdpProbeTimeout}).DialContext,
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	if cerr := resp.Body.Close(); cerr != nil {
		logging.Warn("CDP probe: close response body: %v", cerr)
	}
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// PreflightServer runs server-specific readiness checks before the MCP
// subprocess is spawned. It never returns an error — the goal is to surface
// an actionable hint when a known fragile setup looks misconfigured, while
// still letting the MCP attempt its own connection (Chrome's 144+
// permission API may succeed even when the legacy CDP port is closed).
func PreflightServer(ctx context.Context, cfg ServerConfig) {
	if !isChromeDevToolsMCP(cfg) {
		return
	}
	if cdpReachable(ctx) {
		logging.InfoContext(ctx, "chrome MCP pre-flight: CDP endpoint reachable at %s", cdpEndpoint)
		return
	}
	if chromeUsesAutoConnect(cfg) {
		logging.Warn("chrome MCP pre-flight: CDP endpoint %s unreachable. "+
			"--autoConnect will rely on Chrome 144+'s built-in remote-debugging permission API. "+
			"If chrome tool calls fail with \"Could not find DevToolsActivePort\": (1) ensure Chrome 144+ "+
			"and grant access via chrome://inspect/#remote-debugging → \"Allow remote debugging\" "+
			"(persists for the life of the Chrome process), OR (2) quit Chrome and relaunch with "+
			"--remote-debugging-port=9222 so the legacy attach path works.", cdpEndpoint)
		return
	}
	logging.Warn("chrome MCP pre-flight: CDP endpoint %s unreachable and --autoConnect is not set. "+
		"chrome-devtools-mcp will launch its own Chrome instance with a dedicated profile; "+
		"your existing browser session (cookies, logins) will not be available to the agent. "+
		"Add --autoConnect to attach to your running Chrome instead.", cdpEndpoint)
}
