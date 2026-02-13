#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HARNESS_URL="${HARNESS_URL:-http://localhost:8080}"
CACHE_DIR="$HOME/.cache/creative-mode"

# ── Usage ────────────────────────────────────────────────────────────────────
usage() {
  cat <<'EOF'
Usage: debug.sh <worldID> <subcommand> [args...]

Subcommands (any world):
  status                      World status (template type, build, server)
  list                        List queryable types (client)
  resource <name>             Query a resource by name (client)
  client-query <comp...>      Query components (client)
  client '<json>'             Raw client debug pass-through

Subcommands (2D only):
  room                        Current room + hotspots
  dialog                      Dialog visibility + text
  click <hotspot_id>          Trigger hotspot by ID

Subcommands (3D only):
  query <comp...>             Server ECS query (BRP)
  resources                   List server resources (BRP)
  components <entity>         List components on entity (BRP)
  server '<json>'             Raw server BRP pass-through

Examples:
  debug.sh ef3a75b9 status
  debug.sh ef3a75b9 room
  debug.sh ef3a75b9 click portal
  debug.sh ef3a75b9 dialog
  debug.sh ef3a75b9 query PlayerPosition
  debug.sh ef3a75b9 resources
  debug.sh ef3a75b9 client '{"type":"room"}'

Environment:
  COOKIE        Override session cookie (skip auto-detection)
  HARNESS_URL   Override harness URL (default: http://localhost:8080)
EOF
  exit 0
}

# ── Helpers ──────────────────────────────────────────────────────────────────
die() { echo "Error: $*" >&2; exit 1; }

pretty_json() {
  if command -v python3 &>/dev/null; then
    python3 -m json.tool 2>/dev/null || cat
  else
    cat
  fi
}

# ── Cookie management ────────────────────────────────────────────────────────
get_cookie() {
  # 1. Env var override
  if [[ -n "${COOKIE:-}" ]]; then
    echo "$COOKIE"
    return
  fi

  # 2. Cached cookie (< 5 min old)
  local cache_file="$CACHE_DIR/session-cookie"
  if [[ -f "$cache_file" ]]; then
    local age
    if [[ "$(uname)" == "Darwin" ]]; then
      age=$(( $(date +%s) - $(stat -f %m "$cache_file") ))
    else
      age=$(( $(date +%s) - $(stat -c %Y "$cache_file") ))
    fi
    if (( age < 300 )); then
      cat "$cache_file"
      return
    fi
  fi

  # 3. playwright-cli
  if command -v playwright-cli &>/dev/null; then
    local val
    val=$(playwright-cli cookie-get session 2>/dev/null || true)
    if [[ -n "$val" ]]; then
      mkdir -p "$CACHE_DIR"
      echo "$val" > "$cache_file"
      echo "$val"
      return
    fi
  fi

  die "No session cookie found.
  Option 1: Set COOKIE env var
  Option 2: Run: playwright-cli open $HARNESS_URL --headed --persistent"
}

# ── HTTP helpers ─────────────────────────────────────────────────────────────
http_get() {
  local path="$1"
  local cookie
  cookie=$(get_cookie)
  local status_code body
  body=$(curl -s -w '\n%{http_code}' -b "session=$cookie" "$HARNESS_URL$path")
  status_code=$(echo "$body" | tail -1)
  body=$(echo "$body" | sed '$d')

  case "$status_code" in
    200) echo "$body" ;;
    401) die "Session expired. Run: playwright-cli open $HARNESS_URL --headed --persistent" ;;
    404) die "World '$WORLD_ID' not found" ;;
    *)   die "GET $path failed (HTTP $status_code): $body" ;;
  esac
}

http_post() {
  local path="$1" data="$2"
  local cookie
  cookie=$(get_cookie)
  local status_code body
  body=$(curl -s -w '\n%{http_code}' -X POST -b "session=$cookie" \
    -H 'Content-Type: application/json' \
    "$HARNESS_URL$path" -d "$data")
  status_code=$(echo "$body" | tail -1)
  body=$(echo "$body" | sed '$d')

  case "$status_code" in
    200) echo "$body" ;;
    401) die "Session expired. Run: playwright-cli open $HARNESS_URL --headed --persistent" ;;
    404) die "World '$WORLD_ID' not found" ;;
    503) die "No game server running for this checkpoint" ;;
    504) die "Client debug timed out. Is a browser viewing this world?" ;;
    *)   die "POST $path failed (HTTP $status_code): $body" ;;
  esac
}

# ── Status + type validation ─────────────────────────────────────────────────
fetch_status() {
  http_get "/world/$WORLD_ID/status"
}

get_template_type() {
  local status_json="$1"
  echo "$status_json" | python3 -c "import sys,json; print(json.load(sys.stdin).get('template_type',''))" 2>/dev/null
}

require_type() {
  local required="$1" cmd="$2"
  local status_json
  status_json=$(fetch_status)
  local ttype
  ttype=$(get_template_type "$status_json")

  if [[ "$required" == "2d" && "$ttype" != "2d" ]]; then
    die "'$cmd' is a 2D-only command (world '$WORLD_ID' is $ttype)"
  fi
  if [[ "$required" == "3d" && "$ttype" != "3d" ]]; then
    die "'$cmd' is a 3D-only command (world '$WORLD_ID' is $ttype)"
  fi

  # For 3D server commands, check game_server.running
  if [[ "$required" == "3d" ]]; then
    local running
    running=$(echo "$status_json" | python3 -c "import sys,json; print(json.load(sys.stdin).get('game_server',{}).get('running',False))" 2>/dev/null)
    if [[ "$running" != "True" ]]; then
      die "No game server running for this checkpoint"
    fi
  fi
}

# ── Check harness is reachable ───────────────────────────────────────────────
check_harness() {
  if ! curl -sf "$HARNESS_URL/health" >/dev/null 2>&1; then
    die "Harness not running at $HARNESS_URL. Start with: just -f $ROOT/harness/justfile live"
  fi
}

# ── Args ─────────────────────────────────────────────────────────────────────
if [[ $# -lt 1 ]] || [[ "$1" == "--help" ]] || [[ "$1" == "-h" ]]; then
  usage
fi

WORLD_ID="$1"
shift

if [[ $# -lt 1 ]]; then
  usage
fi

SUBCMD="$1"
shift

check_harness

# ── Subcommand dispatch ──────────────────────────────────────────────────────
case "$SUBCMD" in

  # ── Any world ──────────────────────────────────────────────────────────────
  status)
    fetch_status | pretty_json
    ;;

  list)
    http_post "/world/$WORLD_ID/client-debug" '{"type":"list"}' | pretty_json
    ;;

  resource)
    [[ $# -lt 1 ]] && die "Usage: debug.sh <worldID> resource <name>"
    http_post "/world/$WORLD_ID/client-debug" "{\"type\":\"resource\",\"name\":\"$1\"}" | pretty_json
    ;;

  client-query)
    [[ $# -lt 1 ]] && die "Usage: debug.sh <worldID> client-query <component...>"
    # Build JSON array from args
    components=$(printf '%s\n' "$@" | python3 -c "import sys,json; print(json.dumps([l.strip() for l in sys.stdin]))")
    http_post "/world/$WORLD_ID/client-debug" "{\"type\":\"query\",\"components\":$components}" | pretty_json
    ;;

  client)
    [[ $# -lt 1 ]] && die "Usage: debug.sh <worldID> client '<json>'"
    http_post "/world/$WORLD_ID/client-debug" "$1" | pretty_json
    ;;

  # ── 2D only ────────────────────────────────────────────────────────────────
  room)
    require_type 2d room
    http_post "/world/$WORLD_ID/client-debug" '{"type":"room"}' | pretty_json
    ;;

  dialog)
    require_type 2d dialog
    http_post "/world/$WORLD_ID/client-debug" '{"type":"dialog"}' | pretty_json
    ;;

  click)
    require_type 2d click
    [[ $# -lt 1 ]] && die "Usage: debug.sh <worldID> click <hotspot_id>"
    http_post "/world/$WORLD_ID/client-debug" "{\"type\":\"click\",\"hotspot_id\":\"$1\"}" | pretty_json
    ;;

  # ── 3D only ────────────────────────────────────────────────────────────────
  query)
    require_type 3d query
    [[ $# -lt 1 ]] && die "Usage: debug.sh <worldID> query <component...>"
    components=$(printf '%s\n' "$@" | python3 -c "import sys,json; print(json.dumps([l.strip() for l in sys.stdin]))")
    http_post "/world/$WORLD_ID/debug" \
      "{\"jsonrpc\":\"2.0\",\"method\":\"world.query\",\"id\":1,\"params\":{\"data\":{\"components\":$components}}}" | pretty_json
    ;;

  resources)
    require_type 3d resources
    http_post "/world/$WORLD_ID/debug" \
      '{"jsonrpc":"2.0","method":"world.list_resources","id":1}' | pretty_json
    ;;

  components)
    require_type 3d components
    [[ $# -lt 1 ]] && die "Usage: debug.sh <worldID> components <entity_id>"
    http_post "/world/$WORLD_ID/debug" \
      "{\"jsonrpc\":\"2.0\",\"method\":\"world.list_components\",\"id\":1,\"params\":{\"entity\":$1}}" | pretty_json
    ;;

  server)
    require_type 3d server
    [[ $# -lt 1 ]] && die "Usage: debug.sh <worldID> server '<json>'"
    http_post "/world/$WORLD_ID/debug" "$1" | pretty_json
    ;;

  *)
    die "Unknown subcommand '$SUBCMD'. Run with --help for usage."
    ;;
esac
