#!/usr/bin/env python3
"""Kill proof for the sandbox run API tools.

Each mutant changes one line of internal/tools/runapi_node.go or microvm_runapi.go and a test must fail. The
mutated file never replaces the real one: `go test -overlay` reads it from a scratch directory, so the work tree
stays untouched even when this script is interrupted.

    python3 docs/runapi-kill-proof.py <scratch dir (empty or absent)> [name filter ...]

A mutant that does not build, or whose old text is not found exactly once, is an ERROR and never a kill.
"""
import json
import os
import subprocess
import sys

if len(sys.argv) < 2:
    sys.exit(__doc__)
ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
SCRATCH = os.path.abspath(sys.argv[1])
FILTERS = sys.argv[2:]
if os.path.exists(SCRATCH) and os.listdir(SCRATCH):
    sys.exit("scratch directory must be empty")
os.makedirs(SCRATCH, exist_ok=True)

N = "internal/tools/runapi_node.go"
M = "internal/tools/microvm_runapi.go"
MUTANTS = [
    ("token cache: ignores the 30 s skew", N, "if !runAPINow().Add(runAPIMintSkew + need).Before(s.expires) {", "if !runAPINow().Before(s.expires) {"),
    ("token cache: ignores the call duration", N, "if !runAPINow().Add(runAPIMintSkew + need).Before(s.expires) {", "if !runAPINow().Add(runAPIMintSkew).Before(s.expires) {"),
    ("cache key: shared between callers", N, "return hex.EncodeToString(sum[:]) + \"|\" + sandboxID + \"|\" + strings.Join(scopes, \",\")", "return hex.EncodeToString(sum[:])[:0] + \"|\" + sandboxID + \"|\" + strings.Join(scopes, \",\")"),
    ("cache key: scopes not part of it", N, "return hex.EncodeToString(sum[:]) + \"|\" + sandboxID + \"|\" + strings.Join(scopes, \",\")", "return hex.EncodeToString(sum[:]) + \"|\" + sandboxID"),
    ("node url: plain http accepted", N, "if u.Scheme != \"https\" && !(d.AllowInsecureNodeURL && u.Scheme == \"http\") {", "if false {"),
    ("node url: any path accepted", N, "if strings.TrimRight(u.Path, \"/\") != runAPIPathPrefix {", "if false {"),
    ("node url: credentials accepted", N, "if err != nil || u.Host == \"\" || u.User != nil || u.RawQuery != \"\" || u.Fragment != \"\" {", "if err != nil || u.Host == \"\" {"),
    ("401: retried without a new token", N, "if key != \"\" {\n\t\t\t\trunTokens.drop(key)", "if key != \"\" {\n\t\t\t\t_ = key"),
    ("401: retried forever", N, "&& attempt == 0 {", "&& attempt < 100 {"),
    ("redirects followed", N, "CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },", "CheckRedirect: nil,"),
    ("response size not bounded", N, "if int64(len(raw)) > limit {", "if false {"),
    ("error text: reason not sanitised", N, "Reason: sanitizeGuestText(e.Reason, 64)", "Reason: e.Reason"),
    ("error text: message not sanitised", N, "Message: sanitizeGuestText(e.Message, 200)", "Message: e.Message"),
    ("sanitiser: invalid UTF-8 kept", N, "s = strings.ToValidUTF8(s, ", "_ = strings.ToValidUTF8(s, "),
    ("sanitiser: escape sequences kept", N, "i = skipEscape(rs, i)\n\t\t\tcontinue", "out = append(out, r)"),
    ("sanitiser: carriage return kept", N, "case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):", "case (r < 0x20 && r != '\\r') || r == 0x7f || (r >= 0x80 && r <= 0x9f):"),
    ("sanitiser: C1 controls kept", N, "case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):", "case r < 0x20 || r == 0x7f:"),
    ("sanitiser: osc not skipped to its end", N, "case ']', 'P', 'X', '^', '_':", "case 'P', 'X', '^', '_':"),
    ("head/tail: tail dropped", N, "return head + fmt.Sprintf(\"\\n[... %d bytes omitted ...]\\n\", omitted) + tail, true", "return head + fmt.Sprintf(\"\\n[... %d bytes omitted ...]\\n\", omitted) + tail[:0], true"),
    ("path: relative accepted", N, "if p == \"\" || p[0] != '/' {", "if p == \"\" {"),
    ("path: dot-dot accepted", N, "if seg == \"..\" {", "if seg == \"..\" && false {"),
    ("path: NUL accepted", N, "if strings.ContainsRune(p, 0) {", "if false {"),
    ("exec: both command and argv", M, "if (command == \"\") == (len(argv) == 0) {", "if false {"),
    ("exec: env name unchecked", M, "if !envNameRE.MatchString(k) {", "if false {"),
    ("exec: timeout above the maximum", M, "if timeout < 1 || timeout > execMaxTimeout {", "if timeout < 1 {"),
    ("exec: output cap unchecked", M, "if maxOut < 1 || maxOut > maxMaxOutput {", "if maxOut < 1 {"),
    ("exec: output not capped", M, "out, outCut := headTail(decodeGuestBytes(r.Stdout, r.StdoutEncoding), maxOut)", "out, outCut := decodeGuestBytes(r.Stdout, r.StdoutEncoding), false"),
    ("exec: truncation of the node ignored", M, "StdoutTruncated: r.StdoutTruncated || outCut,", "StdoutTruncated: outCut,"),
    ("exec: not marked untrusted", M, "UntrustedOutput: true,\n\t\tExitCode:        r.ExitCode,", "UntrustedOutput: false,\n\t\tExitCode:        r.ExitCode,"),
    ("exec: foreground not killed on disconnect", M, "body[\"kill_on_disconnect\"] = true", "body[\"kill_on_disconnect\"] = false"),
    ("exec: background flag lost", M, "body[\"background\"] = true", "body[\"background\"] = false"),
    ("exec: wrong scope", M, "resp, err := d.callNode(ctx, cl, in.ID, []string{\"exec\"}, nodeRequest{\n\t\tmethod: http.MethodPost, path: \"/commands\", query:", "resp, err := d.callNode(ctx, cl, in.ID, []string{\"files.read\"}, nodeRequest{\n\t\tmethod: http.MethodPost, path: \"/commands\", query:"),
    ("command id: path traversal accepted", M, "commandIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)", "commandIDRE = regexp.MustCompile(`^.{1,64}$`)"),
    ("kill: any signal", M, "if sig != \"TERM\" && sig != \"KILL\" {", "if false {"),
    ("logs: cursor dropped", M, "q.Set(\"cursor_stdout\", strconv.Itoa(in.Cursor.Stdout))", "_ = in.Cursor"),
    ("logs: streams merged", M, "if e.Stream == \"stderr\" {", "if false {"),
    ("file read: binary accepted", M, "if !utf8.Valid(data) || containsNUL(data) {", "if false {"),
    ("file read: cut rune kept", M, "data = trimIncompleteRune(data)", "_ = data"),
    ("file read: range offset lost", M, "headers: map[string]string{\"Range\": fmt.Sprintf(\"bytes=%d-%d\", in.Offset, in.Offset+int64(length)-1)},", "headers: map[string]string{\"Range\": fmt.Sprintf(\"bytes=0-%d\", int64(length)-1)},"),
    ("file read: length above the maximum", M, "if length < 1 || length > fileReadMax {", "if length < 1 {"),
    ("file read: sanitising not reported", M, "res.Sanitized = clean != string(data)", "res.Sanitized = false"),
    ("file write: size limit", M, "if len(data) > fileWriteMax {", "if false {"),
    ("file write: mode unchecked", M, "if in.Mode != \"\" && !modeRE.MatchString(in.Mode) {\n\t\treturn FileWriteResult{}", "if false {\n\t\treturn FileWriteResult{}"),
    ("file write: overwrite always", M, "\"overwrite\": {strconv.FormatBool(in.Overwrite)}", "\"overwrite\": {\"true\"}"),
    ("file write: wrong scope", M, "resp, err := d.callNode(ctx, cl, in.ID, []string{\"files.write\"}, nodeRequest{\n\t\tmethod: http.MethodPut", "resp, err := d.callNode(ctx, cl, in.ID, []string{\"files.read\"}, nodeRequest{\n\t\tmethod: http.MethodPut"),
    ("file list: depth unchecked", M, "if depth < 1 || depth > 3 {", "if false {"),
    ("file list: limit unchecked", M, "if limit < 1 || limit > listMaxLimit {", "if false {"),
    ("file list: names not sanitised", M, "Name: sanitizeGuestText(e.Name, 1024),", "Name: e.Name,"),
    ("file delete: confirm not required", M, "Register(s, deps, Spec{Name: \"user.microvm.file_delete\", Description: \"Delete a file or directory in a running sandbox. DESTRUCTIVE and irreversible: requires \\\"confirm\\\": true.\", Destructive: true}, deps.fileDelete)", "Register(s, deps, Spec{Name: \"user.microvm.file_delete\", Description: \"Delete a file or directory in a running sandbox. DESTRUCTIVE and irreversible: requires \\\"confirm\\\": true.\"}, deps.fileDelete)"),
    ("file delete: root allowed", M, "if in.Path == \"/\" {", "if false {"),
    ("file delete: recursive always", M, "\"recursive\": {strconv.FormatBool(in.Recursive)}", "\"recursive\": {\"true\"}"),
    ("preview: port range unchecked", M, "if in.Port < 1024 || in.Port > 65535 || in.Port == 49983 {", "if false {"),
]


def run_tests(overlay=None):
    cmd = ["go", "test", "./internal/tools", "-count=1", "-timeout", "120s"]
    if overlay:
        cmd.insert(2, "-overlay=" + overlay)
    r = subprocess.run(cmd, cwd=ROOT, capture_output=True, text=True)
    return r.returncode, r.stdout + r.stderr


rc, out = run_tests()
if rc != 0:
    sys.exit("baseline is red:\n" + out[-1500:])
print("baseline green")
counts = {"KILLED": 0, "SURVIVED": 0, "ERROR": 0}
results = []
for i, (name, rel, old, new) in enumerate(MUTANTS):
    if FILTERS and not any(f in name for f in FILTERS):
        continue
    src = open(os.path.join(ROOT, rel)).read()
    outcome, detail = "ERROR", ""
    if src.count(old) != 1:
        detail = "old text found %d times" % src.count(old)
    else:
        mutated = os.path.join(SCRATCH, "m%d.go" % i)
        open(mutated, "w").write(src.replace(old, new))
        ov = os.path.join(SCRATCH, "overlay%d.json" % i)
        json.dump({"Replace": {os.path.join(ROOT, rel): mutated}}, open(ov, "w"))
        rc, out = run_tests(ov)
        if "[build failed]" in out or "build failed" in out or "cannot use" in out or "declared and not used" in out:
            detail = "does not build"
        elif rc == 0:
            outcome = "SURVIVED"
        elif "--- FAIL" in out or "FAIL\t" in out:
            outcome = "KILLED"
        else:
            detail = out[-200:].replace("\n", " ")
    counts[outcome] += 1
    results.append({"name": name, "outcome": outcome, "detail": detail})
    print("%-9s %s %s" % (outcome, name, detail), flush=True)
json.dump({"counts": counts, "results": results}, open(os.path.join(SCRATCH, "kill-results.json"), "w"), indent=1)
print(counts)
