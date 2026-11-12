package tools_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// The sandbox run API tools against a mock platform (the two mints) and a mock node (the run API itself). The
// secret token must never appear in a tool result or an error.

const secretToken = "v1.k9.SECRETPAYLOADSECRETPAYLOAD.SECRETSIGNATURESECRETSIGNATURESECRETSIGNATURE"

type runFixture struct {
	t        *testing.T
	mu       sync.Mutex
	mints    []map[string]any
	node     []nodeCall
	nodeSrv  *httptest.Server
	mintCode int
	nodeFn   func(w http.ResponseWriter, r *http.Request)
	expires  time.Time
	tokenSeq int
}

type nodeCall struct {
	method, path, query, auth, rng string
	body                           string
}

func newRunFixture(t *testing.T) (*runFixture, *mcp.ClientSession) {
	t.Helper()
	f := &runFixture{t: t, mintCode: http.StatusOK, expires: time.Now().Add(15 * time.Minute)}
	f.nodeSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.node = append(f.node, nodeCall{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), r.Header.Get("Range"), string(raw)})
		fn := f.nodeFn
		f.mu.Unlock()
		if fn != nil {
			fn(w, r)
			return
		}
		writeJSON(w, 404, map[string]any{"success": false, "reason": "not_found", "message": "nothing here"})
	}))
	t.Cleanup(f.nodeSrv.Close)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /microvm/vm/{id}/access", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.mints = append(f.mints, body)
		f.tokenSeq++
		seq := f.tokenSeq
		code := f.mintCode
		exp := f.expires
		f.mu.Unlock()
		if code != http.StatusOK {
			writeJSON(w, code, map[string]any{"success": false, "reason": "sandbox_paused", "message": "the sandbox is paused"})
			return
		}
		writeJSON(w, 200, map[string]any{
			"success": true, "base_url": f.nodeSrv.URL + "/.virtconsole/run/v1", "token": secretToken + strconv.Itoa(seq),
			"token_type": "Bearer", "expires_at": exp.Format(time.RFC3339), "scopes": body["scopes"],
			"sandbox": map[string]any{"id": r.PathValue("id"), "state": "running"},
		})
	})
	mux.HandleFunc("POST /microvm/vm/{id}/preview", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.mints = append(f.mints, body)
		f.mu.Unlock()
		if p, _ := body["port"].(float64); p < 1024 {
			writeJSON(w, 422, map[string]any{"success": false, "reason": "port_not_allowed", "message": "that port cannot be previewed"})
			return
		}
		writeJSON(w, 200, map[string]any{"success": true, "url": "https://3000-abc.sbx.example.com/?vc_preview=" + secretToken, "host": "3000-abc.sbx.example.com", "port": body["port"], "expires_at": "2026-11-11T11:00:00+00:00", "key_id": "k9"})
	})
	master := httptest.NewServer(mux)
	t.Cleanup(master.Close)

	deps := tools.Deps{
		TokenSource: func(_ context.Context) (*client.Client, error) {
			return client.New(master.URL, "platform-bearer", 2*time.Second, false), nil
		},
		RawAPI: func(_ context.Context) (string, string, time.Duration, bool, error) {
			return master.URL, "platform-bearer-" + t.Name(), 2 * time.Second, false, nil
		},
		AllowInsecureNodeURL: true,
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "iaas-mcp-server", Version: "test"}, nil)
	tools.RegisterAll(server, deps)
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	return f, cs
}

func (f *runFixture) calls() []nodeCall {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]nodeCall(nil), f.node...)
}

func noSecret(t *testing.T, res *mcp.CallToolResult) {
	t.Helper()
	b, _ := json.Marshal(res)
	for _, frag := range []string{"SECRETPAYLOAD", "SECRETSIGNATURE", "Bearer v1.", "platform-bearer"} {
		if strings.Contains(string(b), frag) {
			t.Fatalf("a secret leaked into the tool result: %s", frag)
		}
	}
}

func TestExec_MintsOnceCachesAndNeverLeaksTheToken(t *testing.T) {
	f, cs := newRunFixture(t)
	f.nodeFn = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"success": true, "command_id": "cmd-1", "exit_code": 3, "timed_out": false, "stdout": "hello\n", "stderr": "", "stdout_truncated": false, "stderr_truncated": false, "duration_ms": 12})
	}
	for i := 0; i < 2; i++ {
		res := callTool(t, cs, "user.microvm.exec", map[string]any{"id": "sb-1", "command": "echo hello; exit 3"})
		noSecret(t, res)
		var out tools.ExecResult
		unmarshalResult(t, res, &out)
		if !out.UntrustedOutput || out.ExitCode == nil || *out.ExitCode != 3 || out.Stdout != "hello\n" || out.CommandID != "cmd-1" {
			t.Fatalf("result = %+v (a non-zero exit is a normal result)", out)
		}
	}
	if len(f.mints) != 1 {
		t.Fatalf("mints = %d, want 1 (the token is cached per sandbox and scopes)", len(f.mints))
	}
	if sc, _ := f.mints[0]["scopes"].([]any); len(sc) != 1 || sc[0] != "exec" {
		t.Fatalf("scopes = %v", f.mints[0]["scopes"])
	}
	c := f.calls()
	if len(c) != 2 || c[0].path != "/.virtconsole/run/v1/commands" || c[0].query != "stream=false" || !strings.HasPrefix(c[0].auth, "Bearer v1.k9.SECRET") {
		t.Fatalf("node calls = %+v", c)
	}
	var sent map[string]any
	_ = json.Unmarshal([]byte(c[0].body), &sent)
	if sent["command"] != "echo hello; exit 3" || sent["timeout_ms"] != float64(30000) || sent["background"] != false || sent["kill_on_disconnect"] != true {
		t.Fatalf("body = %v", sent)
	}

	// Different scopes are a different token.
	f.nodeFn = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"success": true, "entry": map[string]any{"name": "a", "path": "/a", "type": "file", "size": 1}})
	}
	callTool(t, cs, "user.microvm.file_stat", map[string]any{"id": "sb-1", "path": "/a"})
	if len(f.mints) != 2 {
		t.Fatalf("mints = %d, want 2 after a files.read call", len(f.mints))
	}
}

func TestExec_ValidationHappensBeforeAnyMint(t *testing.T) {
	f, cs := newRunFixture(t)
	bad := []map[string]any{
		{"id": "sb-1"},
		{"id": "sb-1", "command": "a", "argv": []string{"b"}},
		{"id": "sb-1", "command": "a", "timeout_seconds": 121},
		{"id": "sb-1", "command": "a", "max_output_bytes": 65537},
		{"id": "sb-1", "command": "a", "cwd": "relative"},
		{"id": "sb-1", "command": "a", "env": map[string]string{"1BAD": "x"}},
		{"id": "sb-1", "command": "a", "cwd": "/a/../b"},
	}
	for _, args := range bad {
		res := callTool(t, cs, "user.microvm.exec", args)
		if !res.IsError {
			t.Fatalf("args %v: want an error", args)
		}
	}
	if len(f.mints) != 0 || len(f.calls()) != 0 {
		t.Fatalf("a refused input still called out: mints %d node %d", len(f.mints), len(f.calls()))
	}
}

func TestExec_OutputIsSanitisedCappedAndKeepsTheTail(t *testing.T) {
	f, cs := newRunFixture(t)
	long := strings.Repeat("A", 50) + strings.Repeat("M", 100) + "ERROR-AT-THE-END"
	f.nodeFn = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"success": true, "command_id": "c2", "exit_code": nil, "signal": "KILL", "timed_out": true,
			"stdout": "\x1b[31mred\x1b[0m\x1b]0;title\x07 ok\r\x00\x07 bell \xff\xfe", "stderr": long,
			"stdout_truncated": false, "stderr_truncated": false, "duration_ms": 1,
		})
	}
	res := callTool(t, cs, "user.microvm.exec", map[string]any{"id": "sb-1", "argv": []string{"x"}, "max_output_bytes": 60})
	var out tools.ExecResult
	unmarshalResult(t, res, &out)
	if out.Stdout != "red ok bell ��" {
		t.Fatalf("stdout = %q (ANSI, OSC, control characters and invalid UTF-8 must be cleaned)", out.Stdout)
	}
	if out.ExitCode != nil || out.Signal != "KILL" || !out.TimedOut {
		t.Fatalf("result = %+v", out)
	}
	if !out.StderrTruncated || !strings.HasPrefix(out.Stderr, strings.Repeat("A", 30)) || !strings.HasSuffix(out.Stderr, "ERROR-AT-THE-END") || !strings.Contains(out.Stderr, "bytes omitted") {
		t.Fatalf("stderr = %q (head and tail kept, marker between)", out.Stderr)
	}
	if out.StdoutTruncated {
		t.Fatalf("stdout was not cut")
	}
}

func TestExec_Base64OutputIsDecodedThenCleaned(t *testing.T) {
	f, cs := newRunFixture(t)
	f.nodeFn = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"success": true, "command_id": "c3", "exit_code": 0, "stdout": base64.StdEncoding.EncodeToString([]byte("caf\xc3\xa9\x1b[1m!")), "stdout_encoding": "base64", "stderr": ""})
	}
	res := callTool(t, cs, "user.microvm.exec", map[string]any{"id": "sb-1", "command": "x"})
	var out tools.ExecResult
	unmarshalResult(t, res, &out)
	if out.Stdout != "café!" {
		t.Fatalf("stdout = %q", out.Stdout)
	}
}

func TestNodeRefusalsAndMintRefusalsAreToolErrorsWithReasons(t *testing.T) {
	f, cs := newRunFixture(t)
	f.nodeFn = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 409, map[string]any{"success": false, "reason": "sandbox_paused", "message": "The sandbox is paused. Resume it first."})
	}
	res := callTool(t, cs, "user.microvm.exec", map[string]any{"id": "sb-1", "command": "x"})
	if !res.IsError || !strings.Contains(resultText(t, res), "sandbox_paused") {
		t.Fatalf("node refusal = %v %s", res.IsError, resultText(t, res))
	}
	noSecret(t, res)

	f.mu.Lock()
	f.mintCode = http.StatusConflict
	f.mu.Unlock()
	res = callTool(t, cs, "user.microvm.file_read", map[string]any{"id": "sb-2", "path": "/etc/hostname"})
	if !res.IsError || !strings.Contains(resultText(t, res), "sandbox_paused") {
		t.Fatalf("mint refusal = %v %s", res.IsError, resultText(t, res))
	}
	noSecret(t, res)
}

func TestAnExpiredTokenIsRemintedOnceOnA401(t *testing.T) {
	f, cs := newRunFixture(t)
	n := 0
	f.nodeFn = func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			writeJSON(w, 401, map[string]any{"success": false, "reason": "token_expired", "message": "The token has expired."})
			return
		}
		writeJSON(w, 200, map[string]any{"success": true, "command_id": "c", "exit_code": 0, "stdout": "again"})
	}
	res := callTool(t, cs, "user.microvm.exec", map[string]any{"id": "sb-1", "command": "x"})
	var out tools.ExecResult
	unmarshalResult(t, res, &out)
	if out.Stdout != "again" || len(f.mints) != 2 {
		t.Fatalf("stdout %q mints %d (one retry with a new token)", out.Stdout, len(f.mints))
	}
	c := f.calls()
	if c[0].auth == c[1].auth {
		t.Fatalf("the retry reused the rejected token")
	}

	// A second 401 in a row is an error, not a loop.
	f.nodeFn = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 401, map[string]any{"success": false, "reason": "token_revoked", "message": "The token was revoked."})
	}
	res = callTool(t, cs, "user.microvm.exec", map[string]any{"id": "sb-1", "command": "x"})
	if !res.IsError || !strings.Contains(resultText(t, res), "token_revoked") {
		t.Fatalf("second 401 = %v %s", res.IsError, resultText(t, res))
	}
	noSecret(t, res)
}

func TestACachedTokenNearExpiryIsReplaced(t *testing.T) {
	f, cs := newRunFixture(t)
	f.expires = time.Now().Add(20 * time.Second) // inside the 30 s skew: never reusable
	f.nodeFn = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"success": true, "command_id": "c", "exit_code": 0})
	}
	callTool(t, cs, "user.microvm.exec", map[string]any{"id": "sb-1", "command": "x"})
	callTool(t, cs, "user.microvm.exec", map[string]any{"id": "sb-1", "command": "x"})
	if len(f.mints) != 2 {
		t.Fatalf("mints = %d, want 2: a token with under 30 s left is not reused", len(f.mints))
	}
}

func TestBaseURLThatIsNotTheRunAPIIsNeverCalled(t *testing.T) {
	// A node address with another path or a credentialed URL would send the token somewhere else.
	for _, base := range []string{"/other/path", "/.virtconsole/run/v2"} {
		f, cs := newRunFixture(t)
		master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, map[string]any{"success": true, "base_url": f.nodeSrv.URL + base, "token": secretToken, "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339)})
		}))
		defer master.Close()
		deps := tools.Deps{
			TokenSource: func(_ context.Context) (*client.Client, error) {
				return client.New(master.URL, "b", 2*time.Second, false), nil
			},
			AllowInsecureNodeURL: true,
		}
		server := mcp.NewServer(&mcp.Implementation{Name: "x", Version: "t"}, nil)
		tools.RegisterAll(server, deps)
		ct, st := mcp.NewInMemoryTransports()
		ss, _ := server.Connect(context.Background(), st, nil)
		defer ss.Close()
		cs2, _ := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil).Connect(context.Background(), ct, nil)
		defer cs2.Close()
		res := callTool(t, cs2, "user.microvm.exec", map[string]any{"id": "sb-1", "command": "x"})
		if !res.IsError {
			t.Fatalf("base %q: want an error", base)
		}
		if len(f.calls()) != 0 {
			t.Fatalf("base %q: the node was called with the token", base)
		}
		noSecret(t, res)
		_ = cs
	}
}

func TestHTTPSIsRequiredByDefault(t *testing.T) {
	f, _ := newRunFixture(t)
	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"success": true, "base_url": f.nodeSrv.URL + "/.virtconsole/run/v1", "token": secretToken, "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339)})
	}))
	defer master.Close()
	deps := tools.Deps{TokenSource: func(_ context.Context) (*client.Client, error) {
		return client.New(master.URL, "b", 2*time.Second, false), nil
	}}
	server := mcp.NewServer(&mcp.Implementation{Name: "x", Version: "t"}, nil)
	tools.RegisterAll(server, deps)
	ct, st := mcp.NewInMemoryTransports()
	ss, _ := server.Connect(context.Background(), st, nil)
	defer ss.Close()
	cs, _ := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil).Connect(context.Background(), ct, nil)
	defer cs.Close()
	res := callTool(t, cs, "user.microvm.exec", map[string]any{"id": "sb-1", "command": "x"})
	if !res.IsError || len(f.calls()) != 0 {
		t.Fatalf("an http node address must be refused without a call: %v", res.IsError)
	}
}

func TestACallerNeverUsesAnotherCallersCachedToken(t *testing.T) {
	// Two callers of a shared server differ in their platform bearer: the RawAPI source returns it per call.
	var mu sync.Mutex
	who := "alice"
	mints := 0
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"success": true, "command_id": "c", "exit_code": 0})
	}))
	defer node.Close()
	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		mints++
		mu.Unlock()
		writeJSON(w, 200, map[string]any{"success": true, "base_url": node.URL + "/.virtconsole/run/v1", "token": secretToken, "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339)})
	}))
	defer master.Close()
	deps := tools.Deps{
		TokenSource: func(_ context.Context) (*client.Client, error) {
			return client.New(master.URL, "b", 2*time.Second, false), nil
		},
		RawAPI: func(_ context.Context) (string, string, time.Duration, bool, error) {
			mu.Lock()
			defer mu.Unlock()
			return master.URL, "bearer-of-" + who + t.Name(), time.Second, false, nil
		},
		AllowInsecureNodeURL: true,
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "x", Version: "t"}, nil)
	tools.RegisterAll(server, deps)
	ct, st := mcp.NewInMemoryTransports()
	ss, _ := server.Connect(context.Background(), st, nil)
	defer ss.Close()
	cs, _ := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil).Connect(context.Background(), ct, nil)
	defer cs.Close()

	callTool(t, cs, "user.microvm.exec", map[string]any{"id": "shared-sb", "command": "x"})
	callTool(t, cs, "user.microvm.exec", map[string]any{"id": "shared-sb", "command": "x"})
	mu.Lock()
	who = "bob"
	mu.Unlock()
	callTool(t, cs, "user.microvm.exec", map[string]any{"id": "shared-sb", "command": "x"})
	if mints != 2 {
		t.Fatalf("mints = %d, want 2: alice twice from the cache, bob must mint his own", mints)
	}
}

func TestBackgroundStatusLogsKill(t *testing.T) {
	f, cs := newRunFixture(t)
	f.nodeFn = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/.virtconsole/run/v1/commands":
			writeJSON(w, 202, map[string]any{"success": true, "command_id": "bg-1", "pid": 7, "started_at": "2026-11-11T10:00:00Z"})
		case r.URL.Path == "/.virtconsole/run/v1/commands/bg-1" && r.Method == "GET":
			writeJSON(w, 200, map[string]any{"success": true, "command_id": "bg-1", "status": "running", "exit_code": nil, "started_at": "t", "stdout_bytes": 5, "stderr_bytes": 0})
		case r.URL.Path == "/.virtconsole/run/v1/commands/bg-1/logs":
			writeJSON(w, 200, map[string]any{"success": true, "entries": []any{
				map[string]any{"stream": "stdout", "offset": 0, "data": "ab\x1b[2Jcd"},
				map[string]any{"stream": "stderr", "offset": 0, "data": "e", "encoding": "utf8"},
				map[string]any{"stream": "stdout", "offset": 5, "data": base64.StdEncoding.EncodeToString([]byte("ef")), "encoding": "base64"},
			}, "next_cursor": map[string]any{"stdout": 7, "stderr": 1}, "dropped_before": map[string]any{"stdout": 2, "stderr": 0}, "finished": false, "exit_code": nil})
		case r.URL.Path == "/.virtconsole/run/v1/commands/bg-1/kill":
			writeJSON(w, 200, map[string]any{"success": true, "status": "killed"})
		default:
			writeJSON(w, 404, map[string]any{"success": false, "reason": "command_not_found", "message": "No such command."})
		}
	}
	var bg tools.ExecBackgroundResult
	unmarshalResult(t, callTool(t, cs, "user.microvm.exec_background", map[string]any{"id": "sb-1", "argv": []string{"sleep", "100"}, "timeout_seconds": 3600}), &bg)
	if bg.CommandID != "bg-1" {
		t.Fatalf("bg = %+v", bg)
	}
	var sent map[string]any
	_ = json.Unmarshal([]byte(f.calls()[0].body), &sent)
	if sent["background"] != true || sent["timeout_ms"] != float64(3600000) || sent["argv"] == nil {
		t.Fatalf("background body = %v", sent)
	}

	var st tools.CommandStatusResult
	unmarshalResult(t, callTool(t, cs, "user.microvm.command_status", map[string]any{"id": "sb-1", "command_id": "bg-1"}), &st)
	if st.Status != "running" || st.ExitCode != nil || st.StdoutBytes != 5 {
		t.Fatalf("status = %+v", st)
	}

	var lg tools.CommandLogsResult
	unmarshalResult(t, callTool(t, cs, "user.microvm.command_logs", map[string]any{"id": "sb-1", "command_id": "bg-1", "cursor": map[string]any{"stdout": 0, "stderr": 0}}), &lg)
	if lg.Stdout != "abcdef" || lg.Stderr != "e" || lg.NextCursor.Stdout != 7 || lg.DroppedBefore.Stdout != 2 || lg.Finished {
		t.Fatalf("logs = %+v", lg)
	}
	var k tools.CommandKillResult
	unmarshalResult(t, callTool(t, cs, "user.microvm.command_kill", map[string]any{"id": "sb-1", "command_id": "bg-1", "signal": "KILL"}), &k)
	if k.Status != "killed" {
		t.Fatalf("kill = %+v", k)
	}

	for _, args := range []map[string]any{
		{"id": "sb-1", "command_id": "../x"},
		{"id": "sb-1", "command_id": "bg-1", "signal": "HUP"},
	} {
		if !callTool(t, cs, "user.microvm.command_kill", args).IsError {
			t.Fatalf("args %v must be refused", args)
		}
	}
}

func TestFileReadRangesBinaryAndSanitising(t *testing.T) {
	f, cs := newRunFixture(t)
	content := []byte("héllo wörld\r\nline2\x1b[0m\n")
	f.nodeFn = func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.virtconsole/run/v1/files/stat" {
			writeJSON(w, 200, map[string]any{"success": true, "entry": map[string]any{"name": "f", "path": "/f", "type": "file", "size": len(content), "mtime": "2026-11-11T10:00:00Z"}})
			return
		}
		rng := r.Header.Get("Range")
		if rng == "bytes=1000-1099" {
			w.Header().Set("Content-Range", "bytes */"+strconv.Itoa(len(content)))
			writeJSON(w, 416, map[string]any{"success": false, "reason": "range_not_satisfiable", "message": "no"})
			return
		}
		w.Header().Set("Content-Range", "bytes 0-"+strconv.Itoa(len(content)-1)+"/"+strconv.Itoa(len(content)))
		w.WriteHeader(206)
		_, _ = w.Write(content)
	}
	var out tools.FileReadResult
	unmarshalResult(t, callTool(t, cs, "user.microvm.file_read", map[string]any{"id": "sb-1", "path": "/f", "length": 100}), &out)
	if out.Content != "héllo wörld\nline2\n" || !out.Sanitized || out.Size != int64(len(content)) || out.Truncated || out.Mtime == "" || !out.UntrustedOutput {
		t.Fatalf("read = %+v", out)
	}
	if got := f.calls()[0].rng; got != "bytes=0-99" {
		t.Fatalf("Range = %q", got)
	}
	var b64 tools.FileReadResult
	unmarshalResult(t, callTool(t, cs, "user.microvm.file_read", map[string]any{"id": "sb-1", "path": "/f", "encoding": "base64"}), &b64)
	if raw, _ := base64.StdEncoding.DecodeString(b64.Content); string(raw) != string(content) || b64.Sanitized {
		t.Fatalf("base64 must be the exact bytes: %q", b64.Content)
	}
	var past tools.FileReadResult
	unmarshalResult(t, callTool(t, cs, "user.microvm.file_read", map[string]any{"id": "sb-1", "path": "/f", "offset": 1000, "length": 100}), &past)
	if past.Content != "" || past.Truncated || past.Size != int64(len(content)) {
		t.Fatalf("past EOF = %+v", past)
	}

	// Binary content with utf8 requested is refused with a reason; a NUL or invalid UTF-8 counts.
	f.nodeFn = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-3/4")
		w.WriteHeader(206)
		_, _ = w.Write([]byte{0x7f, 'E', 0xff, 0x00})
	}
	res := callTool(t, cs, "user.microvm.file_read", map[string]any{"id": "sb-1", "path": "/bin/x"})
	if !res.IsError || !strings.Contains(resultText(t, res), "binary_content") {
		t.Fatalf("binary = %v %s", res.IsError, resultText(t, res))
	}

	// A range that cuts a multi-byte character gives a clean prefix and a next_offset that continues after it.
	f.nodeFn = func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.virtconsole/run/v1/files/stat" {
			writeJSON(w, 200, map[string]any{"success": true, "entry": map[string]any{}})
			return
		}
		w.Header().Set("Content-Range", "bytes 0-3/9")
		w.WriteHeader(206)
		_, _ = w.Write([]byte("ab\xc3\xa9")[:3]) // 'a','b',0xC3 : half a rune
	}
	var cut tools.FileReadResult
	unmarshalResult(t, callTool(t, cs, "user.microvm.file_read", map[string]any{"id": "sb-1", "path": "/f", "length": 4}), &cut)
	if cut.Content != "ab" || cut.NextOffset != 2 || !cut.Truncated {
		t.Fatalf("cut = %+v", cut)
	}
}

func TestFileWriteListStatMkdirMoveDelete(t *testing.T) {
	f, cs := newRunFixture(t)
	f.nodeFn = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "PUT":
			writeJSON(w, 200, map[string]any{"success": true, "path": "/w/x.txt", "size": 5, "mode": "0644"})
		case r.URL.Path == "/.virtconsole/run/v1/files/list":
			writeJSON(w, 200, map[string]any{"success": true, "truncated": true, "entries": []any{
				map[string]any{"name": "a\x1b[1m\x00b", "path": "/d/a", "type": "symlink", "size": 3, "mode": "0777", "symlink_target": "/etc\x07"},
			}})
		case r.URL.Path == "/.virtconsole/run/v1/files/stat":
			writeJSON(w, 200, map[string]any{"success": true, "entry": map[string]any{"name": "x", "path": "/d/x", "type": "dir", "size": 0}})
		case r.URL.Path == "/.virtconsole/run/v1/files/mkdir", r.URL.Path == "/.virtconsole/run/v1/files/move":
			writeJSON(w, 200, map[string]any{"success": true})
		case r.Method == "DELETE":
			writeJSON(w, 200, map[string]any{"success": true})
		}
	}
	var w tools.FileWriteResult
	unmarshalResult(t, callTool(t, cs, "user.microvm.file_write", map[string]any{"id": "sb-1", "path": "/w/x.txt", "content": "hello", "mode": "0644", "overwrite": true, "create_dirs": true}), &w)
	if w.Size != 5 {
		t.Fatalf("write = %+v", w)
	}
	put := f.calls()[0]
	if put.body != "hello" || !strings.Contains(put.query, "overwrite=true") || !strings.Contains(put.query, "create_dirs=true") || !strings.Contains(put.query, "mode=0644") {
		t.Fatalf("put = %+v", put)
	}
	// base64 content decodes to the bytes; oversize and bad modes are refused before any call.
	callTool(t, cs, "user.microvm.file_write", map[string]any{"id": "sb-1", "path": "/w/b", "content": base64.StdEncoding.EncodeToString([]byte{0, 1, 2}), "encoding": "base64"})
	if last := f.calls()[len(f.calls())-1]; last.body != "\x00\x01\x02" || !strings.Contains(last.query, "overwrite=false") {
		t.Fatalf("base64 put = %+v", last)
	}
	before := len(f.calls())
	for _, args := range []map[string]any{
		{"id": "sb-1", "path": "/w/big", "content": strings.Repeat("x", 1<<20+1)},
		{"id": "sb-1", "path": "/w/m", "content": "x", "mode": "rwx"},
		{"id": "sb-1", "path": "w/rel", "content": "x"},
		{"id": "sb-1", "path": "/w/b", "content": "!!!", "encoding": "base64"},
	} {
		if !callTool(t, cs, "user.microvm.file_write", args).IsError {
			t.Fatalf("args must be refused: %.60v", args)
		}
	}
	if len(f.calls()) != before {
		t.Fatalf("a refused write still called the node")
	}

	var l tools.FileListResult
	unmarshalResult(t, callTool(t, cs, "user.microvm.file_list", map[string]any{"id": "sb-1", "path": "/d", "depth": 2, "limit": 10}), &l)
	if !l.UntrustedOutput || !l.Truncated || len(l.Entries) != 1 || l.Entries[0].Name != "ab" || l.Entries[0].SymlinkTarget != "/etc" {
		t.Fatalf("list = %+v", l)
	}
	for _, args := range []map[string]any{{"id": "sb-1", "path": "/d", "depth": 4}, {"id": "sb-1", "path": "/d", "limit": 501}} {
		if !callTool(t, cs, "user.microvm.file_list", args).IsError {
			t.Fatalf("args %v must be refused", args)
		}
	}
	var st tools.FileStatResult
	unmarshalResult(t, callTool(t, cs, "user.microvm.file_stat", map[string]any{"id": "sb-1", "path": "/d/x"}), &st)
	if st.Entry.Type != "dir" {
		t.Fatalf("stat = %+v", st)
	}
	unmarshalResult(t, callTool(t, cs, "user.microvm.file_mkdir", map[string]any{"id": "sb-1", "path": "/d/n", "parents": false}), &tools.FileMkdirResult{})
	unmarshalResult(t, callTool(t, cs, "user.microvm.file_move", map[string]any{"id": "sb-1", "from": "/a", "to": "/b", "overwrite": true}), &tools.FileMoveResult{})
	calls := f.calls()
	var mk, mv map[string]any
	for _, c := range calls {
		if strings.HasSuffix(c.path, "/files/mkdir") {
			_ = json.Unmarshal([]byte(c.body), &mk)
		}
		if strings.HasSuffix(c.path, "/files/move") {
			_ = json.Unmarshal([]byte(c.body), &mv)
		}
	}
	if mk["parents"] != false || mk["path"] != "/d/n" || mv["from"] != "/a" || mv["overwrite"] != true {
		t.Fatalf("mkdir %v move %v", mk, mv)
	}

	// Delete is confirm-gated and never touches the node without it.
	n := len(f.calls())
	if res := callTool(t, cs, "user.microvm.file_delete", map[string]any{"id": "sb-1", "path": "/d/x"}); !res.IsError || len(f.calls()) != n {
		t.Fatalf("an unconfirmed delete must be refused with no call")
	}
	if res := callTool(t, cs, "user.microvm.file_delete", map[string]any{"id": "sb-1", "path": "/", "confirm": true}); !res.IsError {
		t.Fatalf("deleting / must be refused")
	}
	var d tools.FileDeleteResult
	unmarshalResult(t, callTool(t, cs, "user.microvm.file_delete", map[string]any{"id": "sb-1", "path": "/d/x", "recursive": true, "confirm": true}), &d)
	last := f.calls()[len(f.calls())-1]
	if d.Deleted != "/d/x" || last.method != "DELETE" || !strings.Contains(last.query, "recursive=true") {
		t.Fatalf("delete = %+v %+v", d, last)
	}
}

func TestPreviewURLMintsThroughThePlatformOnly(t *testing.T) {
	f, cs := newRunFixture(t)
	var out tools.PreviewURLResult
	unmarshalResult(t, callTool(t, cs, "user.microvm.preview_url", map[string]any{"id": "sb-1", "port": 3000, "ttl_seconds": 600}), &out)
	if !strings.HasPrefix(out.URL, "https://3000-abc.sbx.example.com/?vc_preview=") || out.Port != 3000 || out.ExpiresAt == "" {
		t.Fatalf("preview = %+v", out)
	}
	if len(f.calls()) != 0 {
		t.Fatalf("a preview mint must not call the node")
	}
	if f.mints[0]["port"] != float64(3000) || f.mints[0]["ttl_seconds"] != float64(600) {
		t.Fatalf("mint body = %v", f.mints[0])
	}
	n := len(f.mints)
	for _, port := range []int{80, 1023, 49983, 65536} {
		if !callTool(t, cs, "user.microvm.preview_url", map[string]any{"id": "sb-1", "port": port}).IsError {
			t.Fatalf("port %d must be refused", port)
		}
	}
	if len(f.mints) != n {
		t.Fatalf("a refused port still reached the platform")
	}
}

func TestNodeErrorTextCarriesNoGuestControlledMarkup(t *testing.T) {
	f, cs := newRunFixture(t)
	f.nodeFn = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 404, map[string]any{"success": false, "reason": "path_not_found", "message": "gone\x1b[31m\x00 /secret"})
	}
	res := callTool(t, cs, "user.microvm.file_stat", map[string]any{"id": "sb-1", "path": "/nope"})
	txt := resultText(t, res)
	if !res.IsError || strings.ContainsAny(txt, "\x1b\x00") || !strings.Contains(txt, "path_not_found") {
		t.Fatalf("error text = %q", txt)
	}
}

// ── tests added from the kill proof ─────────────────────────────────────────

func TestExec_ATokenThatEndsBeforeTheCommandCanIsNotReused(t *testing.T) {
	f, cs := newRunFixture(t)
	f.expires = time.Now().Add(100 * time.Second) // outside the 30 s skew, inside timeout 120 + skew
	f.nodeFn = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"success": true, "command_id": "c", "exit_code": 0})
	}
	for i := 0; i < 2; i++ {
		callTool(t, cs, "user.microvm.exec", map[string]any{"id": "sb-1", "command": "x", "timeout_seconds": 120})
	}
	if len(f.mints) != 2 {
		t.Fatalf("mints = %d, want 2: a response ends when its token does, so the token must outlive the command", len(f.mints))
	}
	// A short command can reuse the same kind of token.
	callTool(t, cs, "user.microvm.exec", map[string]any{"id": "sb-9", "command": "x", "timeout_seconds": 5})
	callTool(t, cs, "user.microvm.exec", map[string]any{"id": "sb-9", "command": "x", "timeout_seconds": 5})
	if len(f.mints) != 3 {
		t.Fatalf("mints = %d, want 3", len(f.mints))
	}
}

func TestNodeURLWithCredentialsIsNeverCalled(t *testing.T) {
	f, _ := newRunFixture(t)
	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"success": true, "base_url": strings.Replace(f.nodeSrv.URL, "http://", "http://user:pw@", 1) + "/.virtconsole/run/v1", "token": secretToken, "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339)})
	}))
	defer master.Close()
	cs := sessionWith(t, master.URL, true, nil)
	res := callTool(t, cs, "user.microvm.exec", map[string]any{"id": "sb-1", "command": "x"})
	if !res.IsError || len(f.calls()) != 0 {
		t.Fatalf("a credentialed node address must be refused without a call")
	}
}

// sessionWith builds a session against a mint server with the given node settings.
func sessionWith(t *testing.T, masterURL string, insecure bool, nodeHTTP *http.Client) *mcp.ClientSession {
	t.Helper()
	deps := tools.Deps{
		TokenSource: func(_ context.Context) (*client.Client, error) {
			return client.New(masterURL, "b", 2*time.Second, false), nil
		},
		AllowInsecureNodeURL: insecure,
		NodeHTTPClient:       nodeHTTP,
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "x", Version: "t"}, nil)
	tools.RegisterAll(server, deps)
	ct, st := mcp.NewInMemoryTransports()
	ss, _ := server.Connect(context.Background(), st, nil)
	t.Cleanup(func() { _ = ss.Close() })
	cs, _ := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil).Connect(context.Background(), ct, nil)
	t.Cleanup(func() { _ = cs.Close() })

	return cs
}

func TestA401IsRetriedOnceAndNoMore(t *testing.T) {
	f, cs := newRunFixture(t)
	f.nodeFn = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 401, map[string]any{"success": false, "reason": "token_invalid", "message": "no"})
	}
	res := callTool(t, cs, "user.microvm.exec", map[string]any{"id": "sb-1", "command": "x"})
	if !res.IsError {
		t.Fatalf("want an error")
	}
	if n := len(f.calls()); n != 2 || len(f.mints) != 2 {
		t.Fatalf("node calls %d mints %d, want 2 and 2", n, len(f.mints))
	}
}

func TestARedirectFromTheNodeIsNeverFollowed(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the redirect target was called, with Authorization %q", r.Header.Get("Authorization"))
	}))
	defer other.Close()
	f, cs := newRunFixture(t)
	f.nodeFn = func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/steal", http.StatusFound)
	}
	res := callTool(t, cs, "user.microvm.exec", map[string]any{"id": "sb-1", "command": "x"})
	if !res.IsError {
		t.Fatalf("a redirect answer is not a success")
	}
	noSecret(t, res)
}

func TestAnOversizeNodeAnswerIsRefusedNotBuffered(t *testing.T) {
	f, cs := newRunFixture(t)
	f.nodeFn = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"stdout":"`))
		_, _ = w.Write([]byte(strings.Repeat("a", 9<<20)))
		_, _ = w.Write([]byte(`"}`))
	}
	res := callTool(t, cs, "user.microvm.exec", map[string]any{"id": "sb-1", "command": "x"})
	if !res.IsError || !strings.Contains(resultText(t, res), "response_too_large") {
		t.Fatalf("oversize = %v %.120s", res.IsError, resultText(t, res))
	}
}

func TestErrorReasonIsSanitisedAndBounded(t *testing.T) {
	f, cs := newRunFixture(t)
	f.nodeFn = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 404, map[string]any{"success": false, "reason": "path\x1b[31m_not_found" + strings.Repeat("z", 200), "message": "m"})
	}
	txt := resultText(t, callTool(t, cs, "user.microvm.file_stat", map[string]any{"id": "sb-1", "path": "/x"}))
	if strings.Contains(txt, "\x1b") || strings.Contains(txt, strings.Repeat("z", 100)) || !strings.Contains(txt, "path_not_found") {
		t.Fatalf("error text = %q", txt)
	}
}

func TestOutputLimitsAndFlagsForExec(t *testing.T) {
	f, cs := newRunFixture(t)
	f.nodeFn = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"success": true, "command_id": "c", "exit_code": 0, "stdout": strings.Repeat("o", 500) + "\u0085\u009b!", "stdout_truncated": true, "stderr": ""})
	}
	var out tools.ExecResult
	unmarshalResult(t, callTool(t, cs, "user.microvm.exec", map[string]any{"id": "sb-1", "command": "x", "max_output_bytes": 100}), &out)
	if len(out.Stdout) > 200 || !strings.Contains(out.Stdout, "bytes omitted") || !out.StdoutTruncated {
		t.Fatalf("stdout len %d truncated %v", len(out.Stdout), out.StdoutTruncated)
	}
	f.nodeFn = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"success": true, "command_id": "c", "exit_code": 0, "stdout": "a\u0085b\u009bc", "stdout_truncated": true})
	}
	unmarshalResult(t, callTool(t, cs, "user.microvm.exec", map[string]any{"id": "sb-1", "command": "x"}), &out)
	if out.Stdout != "abc" || !out.StdoutTruncated {
		t.Fatalf("C1 controls must go and the node's truncation flag must be kept: %+v", out)
	}
}

func TestPathsWithNULAreRefusedBeforeAnyCall(t *testing.T) {
	f, cs := newRunFixture(t)
	if !callTool(t, cs, "user.microvm.file_stat", map[string]any{"id": "sb-1", "path": "/a\u0000b"}).IsError {
		t.Fatalf("a NUL byte in a path must be refused")
	}
	if len(f.mints) != 0 || len(f.calls()) != 0 {
		t.Fatalf("a refused path still called out")
	}
}

func TestLogsCursorAndFileToolScopesAndLimits(t *testing.T) {
	f, cs := newRunFixture(t)
	f.nodeFn = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/logs"):
			writeJSON(w, 200, map[string]any{"success": true, "entries": []any{}, "next_cursor": map[string]any{}, "dropped_before": map[string]any{}, "finished": true})
		default:
			writeJSON(w, 200, map[string]any{"success": true, "path": "/p", "size": 1})
		}
	}
	callTool(t, cs, "user.microvm.command_logs", map[string]any{"id": "sb-1", "command_id": "c1", "cursor": map[string]any{"stdout": 5, "stderr": 3}})
	q := f.calls()[0].query
	if !strings.Contains(q, "cursor_stdout=5") || !strings.Contains(q, "cursor_stderr=3") {
		t.Fatalf("cursor lost: %q", q)
	}
	// Each file tool asks for exactly the scope it needs.
	callTool(t, cs, "user.microvm.file_write", map[string]any{"id": "sb-2", "path": "/p", "content": "x"})
	callTool(t, cs, "user.microvm.file_delete", map[string]any{"id": "sb-3", "path": "/p", "confirm": true})
	got := map[string]string{}
	for _, m := range f.mints {
		got[fmt.Sprint(m["scopes"])] = "x"
	}
	if len(f.mints) != 3 || fmt.Sprint(f.mints[0]["scopes"]) != "[exec]" || fmt.Sprint(f.mints[1]["scopes"]) != "[files.write]" || fmt.Sprint(f.mints[2]["scopes"]) != "[files.write]" {
		t.Fatalf("scopes minted = %v", f.mints)
	}
	last := f.calls()[len(f.calls())-1]
	if !strings.Contains(last.query, "recursive=false") {
		t.Fatalf("delete must not be recursive unless asked: %q", last.query)
	}
	if !callTool(t, cs, "user.microvm.file_read", map[string]any{"id": "sb-1", "path": "/f", "length": 65537}).IsError {
		t.Fatalf("length above 65536 must be refused")
	}
}

func TestMalformedCommandIDsNeverReachTheNode(t *testing.T) {
	f, cs := newRunFixture(t)
	for _, tool := range []string{"user.microvm.command_status", "user.microvm.command_logs", "user.microvm.command_kill"} {
		for _, bad := range []string{"../x", "a/b", "a b", "", strings.Repeat("a", 65), "a?x=1"} {
			if !callTool(t, cs, tool, map[string]any{"id": "sb-1", "command_id": bad}).IsError {
				t.Fatalf("%s accepted %q", tool, bad)
			}
		}
	}
	if len(f.calls()) != 0 || len(f.mints) != 0 {
		t.Fatalf("a malformed command id still called out: node %d mints %d", len(f.calls()), len(f.mints))
	}
}
