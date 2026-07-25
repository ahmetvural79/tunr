#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/tunr-e2e.XXXXXX")"
HOME_DIR="$WORKDIR/home"
MOCK_BIN="$WORKDIR/mockbin"
BIN_DIR="$WORKDIR/bin"
LOG_DIR="$WORKDIR/logs"
mkdir -p "$HOME_DIR" "$MOCK_BIN" "$BIN_DIR" "$LOG_DIR"

cleanup() {
  set +e
  [[ -n "${SHARE_PID:-}" ]] && kill "${SHARE_PID}" 2>/dev/null && wait "${SHARE_PID}" >/dev/null 2>&1
  [[ -n "${START_PID:-}" ]] && kill "${START_PID}" 2>/dev/null && wait "${START_PID}" >/dev/null 2>&1
  [[ -n "${RELAY_PID:-}" ]] && kill "${RELAY_PID}" 2>/dev/null && wait "${RELAY_PID}" >/dev/null 2>&1
  [[ -n "${UP_A_PID:-}" ]] && kill "${UP_A_PID}" 2>/dev/null && wait "${UP_A_PID}" >/dev/null 2>&1
  [[ -n "${UP_B_PID:-}" ]] && kill "${UP_B_PID}" 2>/dev/null && wait "${UP_B_PID}" >/dev/null 2>&1
  [[ -n "${INSPECTOR_PID:-}" ]] && kill "${INSPECTOR_PID}" 2>/dev/null && wait "${INSPECTOR_PID}" >/dev/null 2>&1
  [[ -n "${AUTH_PID:-}" ]] && kill "${AUTH_PID}" 2>/dev/null && wait "${AUTH_PID}" >/dev/null 2>&1
  [[ -n "${UPDATE_PID:-}" ]] && kill "${UPDATE_PID}" 2>/dev/null && wait "${UPDATE_PID}" >/dev/null 2>&1
  if [[ "${FAIL:-0}" -eq 0 ]]; then
    rm -rf "$WORKDIR"
  else
    echo "Preserved E2E workspace for debugging: $WORKDIR"
  fi
}
trap cleanup EXIT

export HOME="$HOME_DIR"
export PATH="$MOCK_BIN:$PATH"
export TUNR_RELAY_URL="http://127.0.0.1:19080"
export TUNR_APP_URL="http://127.0.0.1:19081"
export TUNR_UPDATE_BASE_URL="http://127.0.0.1:19082"
export TUNR_UPDATE_REPO="owner/repo"

PASS=0
FAIL=0

run_case() {
  local name="$1"
  shift
  if "$@"; then
    printf "PASS  %s\n" "$name"
    PASS=$((PASS + 1))
  else
    printf "FAIL  %s\n" "$name"
    FAIL=$((FAIL + 1))
  fi
}

wait_http() {
  local url="$1"
  local tries="${2:-50}"
  local i
  for ((i = 0; i < tries; i++)); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.1
  done
  return 1
}

kill_port_listeners() {
  local port
  for port in "$@"; do
    local pids
    pids="$(lsof -ti tcp:"$port" 2>/dev/null || true)"
    if [[ -n "$pids" ]]; then
      echo "$pids" | xargs kill -9 >/dev/null 2>&1 || true
    fi
  done
}

write_mock_commands() {
  cat >"$MOCK_BIN/security" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
store="${HOME}/.tunr_test_token"
cmd="${1:-}"
shift || true
case "$cmd" in
  add-generic-password)
    token=""
    while [[ $# -gt 0 ]]; do
      case "$1" in
        -w) token="$2"; shift 2 ;;
        *) shift ;;
      esac
    done
    printf '%s' "$token" > "$store"
    ;;
  find-generic-password)
    [[ -f "$store" ]] || exit 44
    cat "$store"
    ;;
  delete-generic-password)
    rm -f "$store"
    ;;
  *) exit 1 ;;
esac
EOF
  chmod +x "$MOCK_BIN/security"

  cat >"$MOCK_BIN/open" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
url="${1:-}"
echo "$url" >> "${TUNR_E2E_OPEN_LOG:-/tmp/tunr-open.log}"
curl -fsS "$url" >/dev/null 2>&1 || true
EOF
  chmod +x "$MOCK_BIN/open"
}

start_upstream_servers() {
  [[ -n "${UP_A_PID:-}" ]] && kill "${UP_A_PID}" 2>/dev/null || true
  [[ -n "${UP_B_PID:-}" ]] && kill "${UP_B_PID}" 2>/dev/null || true
  wait "${UP_A_PID:-}" 2>/dev/null || true
  wait "${UP_B_PID:-}" 2>/dev/null || true
  cat >"$WORKDIR/upstream.py" <<'PY'
import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

NAME = sys.argv[1]
PORT = int(sys.argv[2])

class H(BaseHTTPRequestHandler):
    def do_GET(self): self._handle()
    def do_POST(self): self._handle()
    def do_PUT(self): self._handle()
    def do_DELETE(self): self._handle()
    def log_message(self, *_): pass
    def _handle(self):
        length = int(self.headers.get("Content-Length", "0") or "0")
        body = self.rfile.read(length).decode("utf-8") if length else ""
        payload = {
            "server": NAME,
            "method": self.command,
            "path": self.path,
            "cookie": self.headers.get("Cookie", ""),
            "authorization": self.headers.get("Authorization", ""),
            "body": body,
        }
        if self.path.startswith("/html"):
            data = "<html><body><h1>hello</h1></body></html>"
            self.send_response(200)
            self.send_header("Content-Type", "text/html")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data.encode("utf-8"))
            return
        data = json.dumps(payload).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

HTTPServer(("127.0.0.1", PORT), H).serve_forever()
PY

  python3 "$WORKDIR/upstream.py" a 19101 >"$LOG_DIR/up_a.log" 2>&1 &
  UP_A_PID=$!
  python3 "$WORKDIR/upstream.py" b 19102 >"$LOG_DIR/up_b.log" 2>&1 &
  UP_B_PID=$!
  wait_http "http://127.0.0.1:19101/ready" 100
  wait_http "http://127.0.0.1:19102/ready" 100
}

start_mock_relay() {
  go run ./scripts/e2e/mock_relay.go >"$LOG_DIR/relay.log" 2>&1 &
  RELAY_PID=$!
  wait_http "http://127.0.0.1:19080/api/v1/health" 100
}

start_mock_auth_server() {
  cat >"$WORKDIR/auth_server.py" <<'PY'
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import parse_qs, urlparse, urlencode
import urllib.request

class H(BaseHTTPRequestHandler):
    def log_message(self, *_): pass
    def do_GET(self):
        u = urlparse(self.path)
        if u.path != "/auth/cli":
            self.send_response(404); self.end_headers(); return
        q = parse_qs(u.query)
        state = q.get("state", [""])[0]
        cb = q.get("callback", [""])[0]
        params = urlencode({"token": "e2e-token-123", "state": state})
        urllib.request.urlopen(f"{cb}?{params}", timeout=3)
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"ok")

HTTPServer(("127.0.0.1", 19081), H).serve_forever()
PY
  python3 "$WORKDIR/auth_server.py" >"$LOG_DIR/auth.log" 2>&1 &
  AUTH_PID=$!
  wait_http "http://127.0.0.1:19081/auth/cli?state=x&callback=http://127.0.0.1:1/callback" 3 || true
}

start_mock_inspector_server() {
  cat >"$WORKDIR/inspector_server.py" <<'PY'
import json
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlparse, parse_qs

REQS = [{"id":"req-1","method":"GET","path":"/api/demo","status":200,"duration_ms":12,"time":"now"}]

class H(BaseHTTPRequestHandler):
    def log_message(self, *_): pass
    def do_GET(self):
        u = urlparse(self.path)
        if u.path == "/api/v1/requests":
            body = json.dumps(REQS).encode()
            self.send_response(200); self.send_header("Content-Type","application/json"); self.end_headers(); self.wfile.write(body); return
        if u.path == "/api/v1/health":
            self.send_response(200); self.end_headers(); self.wfile.write(b'{"status":"ok"}'); return
        self.send_response(404); self.end_headers()
    def do_POST(self):
        u = urlparse(self.path)
        if u.path.startswith("/api/v1/requests/"):
            q = parse_qs(u.query)
            action = (q.get("action") or [""])[0]
            if action == "curl":
                self.send_response(200); self.end_headers(); self.wfile.write(b"curl -X GET http://localhost:3000/api/demo"); return
            if action == "replay":
                self.send_response(200); self.send_header("Content-Type","application/json"); self.end_headers(); self.wfile.write(b'{"status_code":200}'); return
        if u.path == "/api/v1/requests" and (parse_qs(u.query).get("action") or [""])[0] == "flush":
            self.send_response(200); self.end_headers(); self.wfile.write(b"ok"); return
        self.send_response(404); self.end_headers()

HTTPServer(("127.0.0.1", 19842), H).serve_forever()
PY
  python3 "$WORKDIR/inspector_server.py" >"$LOG_DIR/inspector.log" 2>&1 &
  INSPECTOR_PID=$!
  wait_http "http://127.0.0.1:19842/api/v1/health" 50
}

start_mock_update_server() {
  local arch
  arch="$(go env GOARCH)"
  if [[ "$arch" == "amd64" ]]; then arch="x86_64"; fi
  local version="9.9.9"
  local tar_file="$WORKDIR/tunr_${version}_$(go env GOOS)_${arch}.tar.gz"
  local checksum_file="$WORKDIR/checksums.txt"

  tar -C "$BIN_DIR" -czf "$tar_file" tunr
  local sum
  sum="$(shasum -a 256 "$tar_file" | awk '{print $1}')"
  printf "%s  %s\n" "$sum" "$(basename "$tar_file")" > "$checksum_file"

  cat >"$WORKDIR/update_server.py" <<PY
from http.server import BaseHTTPRequestHandler, HTTPServer
import os
TAR = r"$tar_file"
SUM = r"$checksum_file"
class H(BaseHTTPRequestHandler):
    def log_message(self, *_): pass
    def do_HEAD(self):
        if self.path == "/owner/repo/releases/latest":
            self.send_response(302)
            self.send_header("Location", "/owner/repo/releases/tag/v9.9.9")
            self.end_headers()
            return
        self.send_response(404); self.end_headers()
    def do_GET(self):
        if self.path.endswith("/checksums.txt"):
            self.send_response(200); self.end_headers(); self.wfile.write(open(SUM,"rb").read()); return
        if self.path.endswith(".tar.gz"):
            self.send_response(200); self.end_headers(); self.wfile.write(open(TAR,"rb").read()); return
        self.send_response(404); self.end_headers()
HTTPServer(("127.0.0.1", 19082), H).serve_forever()
PY
  python3 "$WORKDIR/update_server.py" >"$LOG_DIR/update.log" 2>&1 &
  UPDATE_PID=$!
  wait_http "http://127.0.0.1:19082/owner/repo/releases/download/v9.9.9/checksums.txt" 50
}

start_share() {
  local outfile="$1"
  shift
  "$BIN_DIR/tunr" share --port 19101 --json "$@" >"$outfile" 2>"$outfile.err" &
  SHARE_PID=$!
  for _ in $(seq 1 200); do
    if [[ -s "$outfile" ]]; then
      return 0
    fi
    if ! kill -0 "$SHARE_PID" 2>/dev/null; then
      return 1
    fi
    sleep 0.1
  done
  return 1
}

stop_share() {
  if [[ -n "${SHARE_PID:-}" ]]; then
    kill "$SHARE_PID" 2>/dev/null || true
    wait "$SHARE_PID" 2>/dev/null || true
    unset SHARE_PID
  fi
}

relay_request() {
  local payload="$1"
  curl -fsS -X POST "http://127.0.0.1:19080/_test/request" -H "Content-Type: application/json" -d "$payload"
}

assert_json_contains() {
  local json="$1"
  local needle="$2"
  python3 - "$json" "$needle" <<'PY'
import json, sys
obj = json.loads(sys.argv[1])
s = json.dumps(obj)
sys.exit(0 if sys.argv[2] in s else 1)
PY
}

response_status_eq() {
  local json="$1"
  local expected="$2"
  python3 - "$json" "$expected" <<'PY'
import json, sys
obj = json.loads(sys.argv[1])
sys.exit(0 if int(obj.get("status_code", -1)) == int(sys.argv[2]) else 1)
PY
}

response_body_contains() {
  local json="$1"
  local needle="$2"
  python3 - "$json" "$needle" <<'PY'
import json, sys
obj = json.loads(sys.argv[1])
body = obj.get("body", "")
sys.exit(0 if sys.argv[2] in body else 1)
PY
}

response_header_contains() {
  local json="$1"
  local header="$2"
  local needle="$3"
  python3 - "$json" "$header" "$needle" <<'PY'
import json, sys
obj = json.loads(sys.argv[1])
headers = obj.get("headers", {}) or {}
val = headers.get(sys.argv[2], "")
sys.exit(0 if sys.argv[3] in val else 1)
PY
}

response_body_json_field_eq() {
  local json="$1"
  local field="$2"
  local expected="$3"
  python3 - "$json" "$field" "$expected" <<'PY'
import json, sys
obj = json.loads(sys.argv[1])
body = obj.get("body", "")
try:
    parsed = json.loads(body)
except Exception:
    sys.exit(1)
val = parsed.get(sys.argv[2], "")
sys.exit(0 if str(val) == sys.argv[3] else 1)
PY
}

test_share_basic() {
  local out="$WORKDIR/share-basic.json"
  start_share "$out" --subdomain basic-e2e || return 1
  local resp
  resp="$(relay_request '{"request_id":"r1","method":"GET","path":"/hello","headers":{},"body":""}')" || return 1
  stop_share
  response_status_eq "$resp" 200 && response_body_json_field_eq "$resp" "server" "a" && response_body_json_field_eq "$resp" "path" "/hello"
}

test_share_demo_mode() {
  local out="$WORKDIR/share-demo.json"
  start_share "$out" --demo || return 1
  local resp
  resp="$(relay_request '{"request_id":"r2","method":"POST","path":"/mutate","headers":{"Content-Type":"application/json"},"body":"{\"x\":1}"}')" || return 1
  stop_share
  response_status_eq "$resp" 201 && response_body_contains "$resp" 'demo_success'
}

test_share_widget_injection() {
  local out="$WORKDIR/share-widget.json"
  start_share "$out" --inject-widget || return 1
  local resp
  resp="$(relay_request '{"request_id":"r3","method":"GET","path":"/html","headers":{},"body":""}')" || return 1
  stop_share
  response_status_eq "$resp" 200 && response_body_contains "$resp" 'tunr Vibecoder Feedback Widget'
}

test_share_auto_login() {
  local out="$WORKDIR/share-autologin.json"
  start_share "$out" --auto-login "session=demo-token" || return 1
  local resp
  resp="$(relay_request '{"request_id":"r4","method":"GET","path":"/cookie","headers":{"Cookie":"foo=bar"},"body":""}')" || return 1
  stop_share
  response_status_eq "$resp" 200 && response_body_contains "$resp" 'foo=bar; session=demo-token'
}

test_share_password() {
  local out="$WORKDIR/share-password.json"
  start_share "$out" --password "secret" || return 1
  local noauth
  noauth="$(relay_request '{"request_id":"r5","method":"GET","path":"/private","headers":{},"body":""}')" || return 1
  local auth
  auth="$(relay_request '{"request_id":"r6","method":"GET","path":"/private","headers":{"Authorization":"Basic YWRtaW46c2VjcmV0"},"body":""}')" || return 1
  stop_share
  response_status_eq "$noauth" 401 && response_status_eq "$auth" 200
}

test_share_routing() {
  local out="$WORKDIR/share-routing.json"
  "$BIN_DIR/tunr" share --port 19101 --route /=19101 --route /api=19102 --json >"$out" 2>"$out.err" &
  SHARE_PID=$!
  for _ in $(seq 1 120); do [[ -s "$out" ]] && break; sleep 0.1; done
  local resp
  resp="$(relay_request '{"request_id":"r7","method":"GET","path":"/api/users","headers":{},"body":""}')" || return 1
  stop_share
  response_status_eq "$resp" 200 && response_body_json_field_eq "$resp" "server" "b"
}

test_share_freeze() {
  local out="$WORKDIR/share-freeze.json"
  start_share "$out" --freeze || return 1
  local first
  first="$(relay_request '{"request_id":"r8","method":"GET","path":"/freeze","headers":{},"body":""}')" || return 1
  kill "$UP_A_PID" 2>/dev/null || true
  sleep 0.3
  local second
  second="$(relay_request '{"request_id":"r9","method":"GET","path":"/freeze","headers":{},"body":""}')" || true
  stop_share
  response_status_eq "$first" 200 && response_body_json_field_eq "$first" "server" "a" && response_header_contains "$second" "X-Tunr-Freeze-Cache" "HIT"
}

test_share_ttl() {
  local out="$WORKDIR/share-ttl.json"
  "$BIN_DIR/tunr" share --port 19101 --ttl 2s --json >"$out" 2>"$out.err" &
  SHARE_PID=$!
  for _ in $(seq 1 120); do [[ -s "$out" ]] && break; sleep 0.1; done
  sleep 4
  if kill -0 "$SHARE_PID" 2>/dev/null; then
    stop_share
    return 1
  fi
  unset SHARE_PID
  return 0
}

test_start_status_stop() {
  "$BIN_DIR/tunr" start --port 19101 --subdomain daemon-e2e >"$LOG_DIR/start.log" 2>&1 &
  START_PID=$!
  for _ in $(seq 1 100); do
    if "$BIN_DIR/tunr" status | grep -E "tunr daemon running|daemon-e2e|tunr.test" >/dev/null 2>&1; then
      break
    fi
    sleep 0.2
  done
  "$BIN_DIR/tunr" stop >/dev/null 2>&1 || return 1
  wait "$START_PID" 2>/dev/null || true
  unset START_PID
  "$BIN_DIR/tunr" status | grep -E "No daemon running" >/dev/null
}

test_logs_replay_open() {
  export TUNR_E2E_OPEN_LOG="$LOG_DIR/open.log"
  "$BIN_DIR/tunr" logs >/dev/null 2>&1 || return 1
  "$BIN_DIR/tunr" logs --json | grep -E "req-1|api/demo" >/dev/null || return 1
  "$BIN_DIR/tunr" logs --flush >/dev/null 2>&1 || return 1
  "$BIN_DIR/tunr" replay req-1 --curl | grep -E "^curl -X GET" >/dev/null || return 1
  "$BIN_DIR/tunr" replay req-1 --port 19101 >/dev/null 2>&1 || return 1
  "$BIN_DIR/tunr" open --port 19842 >/dev/null 2>&1 || return 1
  return 0
}

test_login_logout() {
  export TUNR_E2E_OPEN_LOG="$LOG_DIR/open.log"
  "$BIN_DIR/tunr" login >/dev/null 2>&1 || return 1
  [[ -f "$HOME_DIR/.tunr_test_token" ]] || return 1
  "$BIN_DIR/tunr" logout >/dev/null 2>&1 || return 1
  [[ ! -f "$HOME_DIR/.tunr_test_token" ]]
}

test_update() {
  cp "$BIN_DIR/tunr" "$BIN_DIR/tunr-update-target"
  chmod +x "$BIN_DIR/tunr-update-target"
  "$BIN_DIR/tunr-update-target" update >/dev/null 2>&1 || return 1
  [[ -x "$BIN_DIR/tunr-update-target" ]]
}

test_uninstall() {
  cp "$BIN_DIR/tunr" "$BIN_DIR/tunr-uninstall-target"
  chmod +x "$BIN_DIR/tunr-uninstall-target"
  "$BIN_DIR/tunr-uninstall-target" uninstall >/dev/null 2>&1 || return 1
  [[ ! -e "$BIN_DIR/tunr-uninstall-target" ]]
}

test_mcp_protocol() {
  python3 - "$BIN_DIR/tunr" <<'PY'
import json, subprocess, sys
p = subprocess.Popen([sys.argv[1], "mcp"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
def send(obj):
    p.stdin.write(json.dumps(obj) + "\n")
    p.stdin.flush()
    while True:
        line = p.stdout.readline()
        if not line:
            raise RuntimeError("mcp process exited unexpectedly")
        line = line.strip()
        if not line:
            continue
        try:
            return json.loads(line)
        except json.JSONDecodeError:
            continue
r1 = send({"jsonrpc":"2.0","id":1,"method":"initialize","params":{}})
r2 = send({"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}})
r3 = send({"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"tunr_status","arguments":{}}})
p.terminate()
ok = r1.get("result",{}).get("serverInfo",{}).get("name") == "tunr"
ok = ok and any(t.get("name") == "tunr_share" for t in r2.get("result",{}).get("tools",[]))
ok = ok and "No active tunnels" in json.dumps(r3)
sys.exit(0 if ok else 1)
PY
}

test_doctor_smoke() {
  "$BIN_DIR/tunr" doctor >/dev/null 2>&1
}

test_help_surface() {
  "$BIN_DIR/tunr" --help >/dev/null &&
  "$BIN_DIR/tunr" version >/dev/null &&
  "$BIN_DIR/tunr" config init >/dev/null &&
  "$BIN_DIR/tunr" config show >/dev/null
}

main() {
  kill_port_listeners 19080 19081 19082 19101 19102 19842
  write_mock_commands
  go build -trimpath -ldflags="-s -w" -o "$BIN_DIR/tunr" ./cmd/tunr

  start_mock_relay
  start_upstream_servers
  start_mock_auth_server
  start_mock_inspector_server
  start_mock_update_server

  run_case "share basic forwarding" test_share_basic
  run_case "share demo mode blocks mutations" test_share_demo_mode
  run_case "share inject widget html rewrite" test_share_widget_injection
  run_case "share auto-login cookie injection" test_share_auto_login
  run_case "share password protection" test_share_password
  run_case "share path routing" test_share_routing
  run_case "share freeze fallback cache" test_share_freeze
  run_case "share ttl auto-expiry" test_share_ttl

  # restart upstream A after freeze test
  start_upstream_servers

  run_case "start/status/stop daemon flow" test_start_status_stop
  run_case "logs/replay/open commands" test_logs_replay_open
  run_case "login/logout flow with callback" test_login_logout
  run_case "update command self-replace flow" test_update
  run_case "uninstall command side effects" test_uninstall
  run_case "mcp server protocol and tools" test_mcp_protocol
  run_case "doctor command smoke" test_doctor_smoke
  run_case "help/version/config surface" test_help_surface

  echo
  echo "E2E total: $((PASS + FAIL))  pass: $PASS  fail: $FAIL"
  if [[ "$FAIL" -gt 0 ]]; then
    echo "Logs: $LOG_DIR"
    return 1
  fi
}

main "$@"
