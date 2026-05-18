package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	srvmcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// startTestMCPServer returns the URL of a tiny in-process Streamable HTTP
// MCP server with one trivial `echo` tool. The server is shut down on test
// cleanup.
func startTestMCPServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	mcpSrv := server.NewMCPServer("test", "0.0.1", server.WithToolCapabilities(true))
	mcpSrv.AddTool(
		srvmcp.NewTool("echo",
			srvmcp.WithDescription("Echo a message"),
			srvmcp.WithString("message", srvmcp.Required()),
		),
		func(_ context.Context, req srvmcp.CallToolRequest) (*srvmcp.CallToolResult, error) {
			msg, _ := req.RequireString("message")
			return srvmcp.NewToolResultText(msg), nil
		},
	)
	httpSrv := &http.Server{Handler: server.NewStreamableHTTPServer(mcpSrv), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = httpSrv.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
	})
	return "http://" + listener.Addr().String() + "/mcp"
}

func TestParseSingleMCPSpec(t *testing.T) {
	t.Parallel()
	cfg, err := parseSingleMCPSpec("demo:http:https://example.com/mcp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "demo" || cfg.Transport != "streamable_http" || cfg.URL != "https://example.com/mcp" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}

	if _, err := parseSingleMCPSpec("notaspec"); err == nil {
		t.Fatal("expected error for malformed spec")
	}
}

func TestMCPProbeAndTools_Live(t *testing.T) {
	t.Parallel()
	url := startTestMCPServer(t)
	spec := "demo:http:" + url

	// probe
	probe := newMCPProbeCmd()
	var probeOut bytes.Buffer
	probe.SetOut(&probeOut)
	probe.SetErr(&probeOut)
	probe.SetContext(context.Background())
	probe.SetArgs([]string{spec})
	if err := probe.Execute(); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !strings.Contains(probeOut.String(), "1 tools") && !strings.Contains(probeOut.String(), "1 tool") {
		t.Fatalf("probe output missing tool count:\n%s", probeOut.String())
	}

	// tools (plain)
	tools := newMCPToolsCmd()
	var toolsOut bytes.Buffer
	tools.SetOut(&toolsOut)
	tools.SetErr(&toolsOut)
	tools.SetContext(context.Background())
	tools.SetArgs([]string{spec})
	if err := tools.Execute(); err != nil {
		t.Fatalf("tools: %v", err)
	}
	if !strings.Contains(toolsOut.String(), "echo — Echo a message") {
		t.Fatalf("tools output missing echo:\n%s", toolsOut.String())
	}

	// tools --json
	toolsJSON := newMCPToolsCmd()
	var jsonOut bytes.Buffer
	toolsJSON.SetOut(&jsonOut)
	toolsJSON.SetErr(&jsonOut)
	toolsJSON.SetContext(context.Background())
	toolsJSON.SetArgs([]string{spec, "--json"})
	if err := toolsJSON.Execute(); err != nil {
		t.Fatalf("tools --json: %v", err)
	}
	body := jsonOut.String()
	for _, want := range []string{`"name": "echo"`, `"input_schema"`, `"required"`} {
		if !strings.Contains(body, want) {
			t.Errorf("json output missing %q\n%s", want, body)
		}
	}
}

func TestMCPListReadsManifest(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "agents", "demo")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := `name: demo
version: 1
working_dir: none
prompt: "hi"
mcp_servers:
  - name: srv-a
    transport: streamable_http
    url: https://example.com/a
  - name: srv-b
    transport: sse
    url: https://example.com/b
`
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	cmd := newMCPListCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--agent", "demo", "--agents-dir", filepath.Join(tmp, "agents")})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("ls: %v", err)
	}
	body := out.String()
	for _, want := range []string{
		"declares 2 MCP server",
		"srv-a",
		"streamable_http: https://example.com/a",
		"srv-b",
		"sse: https://example.com/b",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("ls output missing %q\n%s", want, body)
		}
	}
}

func TestMCPListNoAgent(t *testing.T) {
	t.Parallel()
	cmd := newMCPListCmd()
	cmd.SetContext(context.Background())
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{}) // no --agent
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--agent is required") {
		t.Fatalf("expected --agent required error, got: %v", err)
	}
}

func TestMCPListZeroServers(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "agents", "empty")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := "name: empty\nversion: 1\nworking_dir: none\nprompt: hi\n"
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	cmd := newMCPListCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--agent", "empty", "--agents-dir", filepath.Join(tmp, "agents")})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("ls: %v", err)
	}
	if !strings.Contains(out.String(), "declares no MCP servers") {
		t.Fatalf("expected no-servers message, got:\n%s", out.String())
	}
}

func TestNewMCPCmdWiring(t *testing.T) {
	t.Parallel()
	root := newMCPCmd()
	want := map[string]bool{"ls": false, "probe": false, "tools": false}
	for _, sub := range root.Commands() {
		if _, ok := want[sub.Name()]; ok {
			want[sub.Name()] = true
		}
	}
	for name, present := range want {
		if !present {
			t.Errorf("expected subcommand %q registered under mcp", name)
		}
	}
}

// ensure that the MCP parent command has a Short description so help works.
func TestNewMCPCmdHasShort(t *testing.T) {
	t.Parallel()
	if newMCPCmd().Short == "" {
		t.Fatal("mcp command missing Short description")
	}
}

// guard against future regressions where probe gets called without arg.
func TestMCPProbeRequiresArg(t *testing.T) {
	t.Parallel()
	cmd := newMCPProbeCmd()
	cmd.SetContext(context.Background())
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for missing SPEC")
	}
}

// closeMCPClient must be nil-safe.
func TestCloseMCPClientNil(t *testing.T) {
	t.Parallel()
	closeMCPClient(nil)
}

// emitToolJSON should produce a parseable, indented JSON object.
func TestEmitToolJSON(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	schema := map[string]any{"type": "object"}
	if err := emitToolJSON(&buf, "x", "d", schema); err != nil {
		t.Fatalf("emitToolJSON: %v", err)
	}
	if !strings.Contains(buf.String(), `"name": "x"`) || !strings.Contains(buf.String(), `"description": "d"`) {
		t.Fatalf("unexpected output:\n%s", buf.String())
	}
}

// TestMCPProbeBadSpec exercises the parseSingleMCPSpec error return in probe.
func TestMCPProbeBadSpec(t *testing.T) {
	t.Parallel()
	cmd := newMCPProbeCmd()
	cmd.SetContext(context.Background())
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"notaspec"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "invalid MCP server spec") {
		t.Fatalf("expected invalid-spec error, got: %v", err)
	}
}

// TestMCPToolsBadSpec covers the parseSingleMCPSpec error return in tools.
func TestMCPToolsBadSpec(t *testing.T) {
	t.Parallel()
	cmd := newMCPToolsCmd()
	cmd.SetContext(context.Background())
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"notaspec"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "invalid MCP server spec") {
		t.Fatalf("expected invalid-spec error, got: %v", err)
	}
}

// TestMCPToolsBadURL covers the Connect error return in tools.
func TestMCPToolsBadURL(t *testing.T) {
	t.Parallel()
	cmd := newMCPToolsCmd()
	cmd.SetContext(context.Background())
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	cmd.SetArgs([]string{fmt.Sprintf("dead:http:http://%s/mcp", addr)})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error connecting to dead port")
	}
}

// TestParseSingleMCPSpecEmptyName covers the cfg.Name == "" branch.
func TestParseSingleMCPSpecEmptyName(t *testing.T) {
	t.Parallel()
	_, err := parseSingleMCPSpec(":sse:http://example.com")
	if err == nil || !strings.Contains(err.Error(), "missing name") {
		t.Fatalf("expected missing-name error, got: %v", err)
	}
}

// TestMCPListFindAgentDirError covers the FindAgentDir error return in `ls`
// — no --agents-dir and no config available, so FindAgentDir errors out.
func TestMCPListFindAgentDirError(t *testing.T) {
	t.Parallel()
	cmd := newMCPListCmd()
	cmd.SetContext(context.Background())
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"--agent", "missing"}) // no --agents-dir, no config
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when FindAgentDir cannot locate agent")
	}
}

// TestMCPListLoadManifestError covers the LoadManifest error return.
func TestMCPListLoadManifestError(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "broken")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("bad: ["), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cmd := newMCPListCmd()
	cmd.SetContext(context.Background())
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"--agent", "broken", "--agents-dir", tmp})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected LoadManifest error")
	}
}

// errorWriter always fails on Write — used to exercise Fprintf error paths.
type errorWriter struct{}

func (errorWriter) Write(_ []byte) (int, error) { return 0, fmt.Errorf("write fails") }

// TestMCPListFprintfErrors covers Fprintf failure branches in `ls`.
func TestMCPListFprintfErrors(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "demo")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := "name: demo\nversion: 1\nworking_dir: none\nprompt: hi\nmcp_servers:\n  - name: srv\n    transport: sse\n    url: https://example.com\n"
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cmd := newMCPListCmd()
	cmd.SetContext(context.Background())
	cmd.SetOut(errorWriter{})
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"--agent", "demo", "--agents-dir", tmp})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected Fprintf error to propagate")
	}
}

// TestMCPListZeroServersFprintfError covers the zero-servers Fprintf path.
func TestMCPListZeroServersFprintfError(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "empty")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("name: empty\nversion: 1\nworking_dir: none\nprompt: hi\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cmd := newMCPListCmd()
	cmd.SetContext(context.Background())
	cmd.SetOut(errorWriter{})
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"--agent", "empty", "--agents-dir", tmp})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected Fprintf error to propagate")
	}
}

// TestMCPToolsFprintfErrorOnHeader covers the header Fprintf failure in tools.
func TestMCPToolsFprintfErrorOnHeader(t *testing.T) {
	t.Parallel()
	url := startTestMCPServer(t)
	cmd := newMCPToolsCmd()
	cmd.SetContext(context.Background())
	cmd.SetOut(errorWriter{})
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"demo:http:" + url})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected Fprintf error to propagate")
	}
}

// brokenAfterOneWriter succeeds on the first Write call then fails. Used so
// the tools-loop body executes the per-tool Fprintf and surfaces an error.
type brokenAfterOneWriter struct {
	count int
}

func (w *brokenAfterOneWriter) Write(p []byte) (int, error) {
	w.count++
	if w.count > 1 {
		return 0, fmt.Errorf("simulated write failure")
	}
	return len(p), nil
}

// TestMCPToolsFprintfErrorOnRow covers the per-tool Fprintf failure path.
func TestMCPToolsFprintfErrorOnRow(t *testing.T) {
	t.Parallel()
	url := startTestMCPServer(t)
	cmd := newMCPToolsCmd()
	cmd.SetContext(context.Background())
	cmd.SetOut(&brokenAfterOneWriter{})
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"demo:http:" + url})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected per-row Fprintf error to propagate")
	}
}

// TestMCPToolsJSONFprintfError covers the JSON-mode emitToolJSON error path.
func TestMCPToolsJSONFprintfError(t *testing.T) {
	t.Parallel()
	url := startTestMCPServer(t)
	cmd := newMCPToolsCmd()
	cmd.SetContext(context.Background())
	cmd.SetOut(&brokenAfterOneWriter{})
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"demo:http:" + url, "--json"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected emitToolJSON Fprintf error to propagate")
	}
}

// TestEmitToolJSONMarshalError covers the MarshalIndent error branch — a
// schema containing a channel cannot be JSON-encoded.
func TestEmitToolJSONMarshalError(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := emitToolJSON(&buf, "x", "d", make(chan int))
	if err == nil {
		t.Fatal("expected MarshalIndent error for unsupported type")
	}
}

// ensure probe surfaces transport errors instead of swallowing them.
func TestMCPProbeBadURL(t *testing.T) {
	t.Parallel()
	cmd := newMCPProbeCmd()
	cmd.SetContext(context.Background())
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	// Bind a listener, close it, then point the client at the dead port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	cmd.SetArgs([]string{fmt.Sprintf("dead:http:http://%s/mcp", addr)})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error connecting to dead port")
	}
}
