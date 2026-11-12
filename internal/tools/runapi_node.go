package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// The sandbox run API. A tool mints a short-lived token from the
// platform (POST /microvm/vm/{id}/access) and then calls the NODE that hosts the sandbox itself; nothing is relayed
// through the platform. This file is the node side: the mint cache, the HTTP calls, the error type and the one
// sanitiser every piece of guest-controlled text goes through.
//
// Rules this file keeps:
//   - the token is held in this process only (cache), sent in an Authorization header and nowhere else: it is not
//     returned by a tool, not put in an error, not logged, not in a URL;
//   - a redirect is never followed (the Authorization header would follow it);
//   - the node's base URL must be https with the fixed run API path, so a wrong answer cannot send a token
//     elsewhere (tests may opt out through Deps.AllowInsecureNodeURL);
//   - every response body is read through a size limit.

const (
	runAPIPathPrefix  = "/.virtconsole/run/v1"
	runAPIMintSkew    = 30 * time.Second
	runAPIMaxBody     = 8 << 20 // JSON bodies from the node
	runAPIMaxFileRead = 1 << 20 // bytes of file content a tool asks for at most is far below this
)

var runAPINow = time.Now

// runScopes is every scope a minted token can carry, in the platform's canonical order.
var runScopeOrder = []string{"exec", "files.read", "files.write"}

// NodeError is a refusal from the node (or a transport failure reaching it). Its text is built from the node's
// fixed reason code and fixed message only, run through the sanitiser, and never from a header or a URL.
type NodeError struct {
	Status  int
	Reason  string
	Message string
}

func (e *NodeError) Error() string {
	var b strings.Builder
	b.WriteString("sandbox run API refused the request")
	if e.Reason != "" {
		b.WriteString(": ")
		b.WriteString(e.Reason)
	}
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	if e.Status != 0 {
		fmt.Fprintf(&b, " (HTTP %d)", e.Status)
	}
	return b.String()
}

// runSession is one minted token and where to use it.
type runSession struct {
	baseURL string
	token   string
	expires time.Time
}

type runCache struct {
	mu sync.Mutex
	m  map[string]runSession
}

var runTokens = &runCache{m: map[string]runSession{}}

func (c *runCache) get(key string, need time.Duration) (runSession, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.m[key]
	if !ok {
		return runSession{}, false
	}
	if !runAPINow().Add(runAPIMintSkew + need).Before(s.expires) {
		delete(c.m, key)
		return runSession{}, false
	}
	return s, true
}

func (c *runCache) put(key string, s runSession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) > 256 { // a runaway of distinct sandboxes: forget the expired, then the rest
		now := runAPINow()
		for k, v := range c.m {
			if !now.Before(v.expires) {
				delete(c.m, k)
			}
		}
		if len(c.m) > 256 {
			c.m = map[string]runSession{}
		}
	}
	c.m[key] = s
}

func (c *runCache) drop(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, key)
}

// cacheKey names one caller, one sandbox and one scope set. The caller part is a hash of the platform endpoint and
// the caller's own bearer token, so two callers of a shared server never share a run token. Without a raw source
// there is no caller identity and nothing is cached.
func (d Deps) runCacheKey(ctx context.Context, sandboxID string, scopes []string) string {
	if d.RawAPI == nil {
		return ""
	}
	endpoint, token, _, _, err := d.RawAPI(ctx)
	if err != nil || token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(endpoint + "\x00" + token))

	return hex.EncodeToString(sum[:]) + "|" + sandboxID + "|" + strings.Join(scopes, ",")
}

func (d Deps) nodeHTTP() *http.Client {
	if d.NodeHTTPClient != nil {
		return d.NodeHTTPClient
	}

	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// runSessionFor returns a usable token for the sandbox with exactly these scopes, minting one when the cache has
// none that lasts another 30 seconds plus `need` (the longest the call can run: a response ends when the token
// that started it expires).
func (d Deps) runSessionFor(ctx context.Context, cl *client.Client, sandboxID string, scopes []string, need time.Duration) (runSession, string, error) {
	scopes = canonicalScopes(scopes)
	key := d.runCacheKey(ctx, sandboxID, scopes)
	if key != "" {
		if s, ok := runTokens.get(key, need); ok {
			return s, key, nil
		}
	}
	out, err := cl.MintMicrovmAccess(ctx, sandboxID, client.MicrovmAccessRequest{Scopes: scopes, TTLSeconds: 900})
	if err != nil {
		return runSession{}, key, err
	}
	token, _ := out["token"].(string)
	base, _ := out["base_url"].(string)
	if err := d.checkNodeURL(base); err != nil {
		return runSession{}, key, err
	}
	exp := runAPINow().Add(5 * time.Minute)
	if s, _ := out["expires_at"].(string); s != "" {
		if t, perr := time.Parse(time.RFC3339, s); perr == nil {
			exp = t
		}
	}
	s := runSession{baseURL: strings.TrimRight(base, "/"), token: token, expires: exp}
	if key != "" {
		runTokens.put(key, s)
	}

	return s, key, nil
}

// checkNodeURL refuses any base URL that is not https on a host with the fixed run API path.
func (d Deps) checkNodeURL(base string) error {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("the platform returned a sandbox run API address this server will not call")
	}
	if u.Scheme != "https" && !(d.AllowInsecureNodeURL && u.Scheme == "http") {
		return errors.New("the platform returned a sandbox run API address this server will not call (not https)")
	}
	if strings.TrimRight(u.Path, "/") != runAPIPathPrefix {
		return errors.New("the platform returned a sandbox run API address this server will not call (unexpected path)")
	}

	return nil
}

func canonicalScopes(in []string) []string {
	have := map[string]bool{}
	for _, s := range in {
		have[s] = true
	}
	var out []string
	for _, s := range runScopeOrder {
		if have[s] {
			out = append(out, s)
		}
	}

	return out
}

// nodeRequest describes one call to the node.
type nodeRequest struct {
	method  string
	path    string // starts with /, below the run API prefix
	query   url.Values
	body    []byte
	ctype   string
	headers map[string]string
	timeout time.Duration
	// maxBody bounds the response body that is read; 0 = runAPIMaxBody.
	maxBody int64
}

// nodeResponse is a completed call whose status is 2xx (or 304/416 which the caller asked to see).
type nodeResponse struct {
	status int
	header http.Header
	body   []byte
}

// callNode performs the call with a token for the scopes. A 401 means the node did not run the request, so it is
// retried once with a freshly minted token (the cached one expired early or was revoked).
func (d Deps) callNode(ctx context.Context, cl *client.Client, sandboxID string, scopes []string, req nodeRequest, accept ...int) (*nodeResponse, error) {
	for attempt := 0; ; attempt++ {
		sess, key, err := d.runSessionFor(ctx, cl, sandboxID, scopes, req.timeout)
		if err != nil {
			return nil, err
		}
		resp, err := d.doNode(ctx, sess, req, accept)
		var ne *NodeError
		if errors.As(err, &ne) && ne.Status == http.StatusUnauthorized && attempt == 0 {
			if key != "" {
				runTokens.drop(key)
			} else {
				return nil, err
			}

			continue
		}

		return resp, err
	}
}

func (d Deps) doNode(ctx context.Context, sess runSession, req nodeRequest, accept []int) (*nodeResponse, error) {
	target := sess.baseURL + req.path
	if len(req.query) > 0 {
		target += "?" + req.query.Encode()
	}
	timeout := req.timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var body io.Reader
	if req.body != nil {
		body = bytes.NewReader(req.body)
	}
	hr, err := http.NewRequestWithContext(cctx, req.method, target, body)
	if err != nil {
		return nil, errors.New("could not build the sandbox run API request")
	}
	hr.Header.Set("Authorization", "Bearer "+sess.token)
	hr.Header.Set("Accept", "application/json")
	if req.ctype != "" {
		hr.Header.Set("Content-Type", req.ctype)
	}
	for k, v := range req.headers {
		hr.Header.Set(k, v)
	}
	res, err := d.nodeHTTP().Do(hr)
	if err != nil {
		// A transport error from net/http names the URL; it holds no token (the token is a header), but the
		// reason is kept generic anyway: the caller needs to know the node was unreachable, not the plumbing.
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, &NodeError{Reason: "node_timeout", Message: "the node did not answer in time"}
		}

		return nil, &NodeError{Reason: "node_unreachable", Message: "the node could not be reached"}
	}
	defer res.Body.Close()
	limit := req.maxBody
	if limit <= 0 {
		limit = runAPIMaxBody
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, &NodeError{Status: res.StatusCode, Reason: "node_read_failed", Message: "the answer of the node could not be read"}
	}
	if int64(len(raw)) > limit {
		return nil, &NodeError{Status: res.StatusCode, Reason: "response_too_large", Message: "the answer of the node is larger than this server reads"}
	}
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return &nodeResponse{status: res.StatusCode, header: res.Header, body: raw}, nil
	}
	for _, a := range accept {
		if res.StatusCode == a {
			return &nodeResponse{status: res.StatusCode, header: res.Header, body: raw}, nil
		}
	}
	var e struct {
		Reason  string `json:"reason"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &e)

	return nil, &NodeError{Status: res.StatusCode, Reason: sanitizeGuestText(e.Reason, 64), Message: sanitizeGuestText(e.Message, 200)}
}

// ── the sanitiser ───────────────────────────────────────────────────────────

// sanitizeGuestText makes text the guest controls safe to hand to an agent: invalid UTF-8 becomes U+FFFD, ANSI
// escape sequences (CSI, OSC and the two-byte forms) are removed, and every control character except newline and tab
// is dropped (carriage return, NUL, other C0, DEL and the C1 range). It is a mitigation, not a guarantee: the text
// can still say anything, which is why every tool output carries untrusted_output:true. maxRunes 0 = no cut.
func sanitizeGuestText(s string, maxRunes int) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	rs := []rune(s)
	out := make([]rune, 0, len(rs))
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == 0x1b:
			i = skipEscape(rs, i)
			continue
		case r == '\n' || r == '\t':
			out = append(out, r)
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			continue
		default:
			out = append(out, r)
		}
		if maxRunes > 0 && len(out) >= maxRunes {
			break
		}
	}

	return string(out)
}

// skipEscape returns the index of the last rune of the escape sequence that starts at rs[i] (an ESC).
func skipEscape(rs []rune, i int) int {
	if i+1 >= len(rs) {
		return i
	}
	switch rs[i+1] {
	case '[': // CSI: parameters 0x30-0x3f, intermediates 0x20-0x2f, one final byte 0x40-0x7e
		j := i + 2
		for j < len(rs) && rs[j] >= 0x30 && rs[j] <= 0x3f {
			j++
		}
		for j < len(rs) && rs[j] >= 0x20 && rs[j] <= 0x2f {
			j++
		}
		if j < len(rs) && rs[j] >= 0x40 && rs[j] <= 0x7e {
			return j
		}

		return j - 1
	case ']', 'P', 'X', '^', '_': // OSC, DCS, SOS, PM, APC: up to BEL or ST (ESC \)
		j := i + 2
		for j < len(rs) {
			if rs[j] == 0x07 {
				return j
			}
			if rs[j] == 0x1b && j+1 < len(rs) && rs[j+1] == '\\' {
				return j + 1
			}
			j++
		}

		return len(rs) - 1
	default: // ESC followed by one character (and optional intermediates)
		j := i + 1
		for j+1 < len(rs) && rs[j] >= 0x20 && rs[j] <= 0x2f {
			j++
		}

		return j
	}
}

// decodeGuestBytes turns node text (utf8 or base64 of raw bytes) into sanitised text.
func decodeGuestBytes(data, encoding string) string {
	if encoding == "base64" {
		raw, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			raw, err = base64.RawStdEncoding.DecodeString(data)
			if err != nil {
				return ""
			}
		}

		return sanitizeGuestText(string(raw), 0)
	}

	return sanitizeGuestText(data, 0)
}

// headTail keeps the first and last halves of s when it is longer than max bytes and says how much it left out.
// The tail usually holds the error, so it is never the part that is cut. Cuts fall on rune boundaries.
func headTail(s string, max int) (string, bool) {
	if max <= 0 || len(s) <= max {
		return s, false
	}
	half := max / 2
	head := s[:half]
	for len(head) > 0 && !utf8.ValidString(head) {
		head = head[:len(head)-1]
	}
	tail := s[len(s)-half:]
	for len(tail) > 0 && !utf8.ValidString(tail) {
		tail = tail[1:]
	}
	omitted := len(s) - len(head) - len(tail)

	return head + fmt.Sprintf("\n[... %d bytes omitted ...]\n", omitted) + tail, true
}

// validateGuestPath mirrors the node's path rules so a refusal is clear before any call is made.
func validateGuestPath(p string) error {
	if p == "" || p[0] != '/' {
		return errors.New("path must be absolute (start with /)")
	}
	if len(p) > 4096 {
		return errors.New("path is longer than 4096 bytes")
	}
	if strings.ContainsRune(p, 0) {
		return errors.New("path contains a NUL byte")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return errors.New("path must not contain a .. segment")
		}
	}

	return nil
}
