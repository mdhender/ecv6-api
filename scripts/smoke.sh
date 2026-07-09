#!/usr/bin/env bash
#
# smoke.sh — end-to-end smoke test of the EC API driven by the earl client.
#
# It is hermetic: it builds the binaries, starts a throwaway in-memory server on
# its own port (ec serve --memory, which auto-seeds a well-known admin and never
# touches disk), drives the application surface with earl, and tears everything
# down. It touches none of your real data — not games/alpha, not games/claude,
# not ~/.config/earl.
#
# Usage:
#   scripts/smoke.sh
#
# Overrides (environment variables):
#   PORT           listen port (default: a free ephemeral port, else 8099)
#
# Exit status is the number of failed checks (0 = all passed).

set -uo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

PORT="${PORT:-$(python3 -c 'import socket; s=socket.socket(); s.bind(("",0)); print(s.getsockname()[1]); s.close()' 2>/dev/null || echo 8099)}"

# The well-known admin ec serve --memory auto-seeds (see cmd/ec/serve.go).
ADMIN_EMAIL="admin@ecv6.example.com"
ADMIN_SECRET="password"

WORK=$(mktemp -d)
BIN="$WORK/bin"
export XDG_CONFIG_HOME="$WORK/config" # isolate earl's tokens.json from ~/.config
mkdir -p "$BIN" "$XDG_CONFIG_HOME"

SERVER_PID=""
cleanup() {
	[ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null
	rm -rf "$WORK"
}
trap cleanup EXIT

echo "building ec, earl ..."
go build -o "$BIN/ec" ./cmd/ec
go build -o "$BIN/earl" ./cmd/earl

# Blank EC_DATA so a repo .env* pointing at a real database does not collide with
# --memory (the two are mutually exclusive); godotenv won't override an already-set
# variable, so exporting it empty here wins.
export EC_DATA=""

# --memory serves a fresh migrated in-memory database seeded with the well-known
# admin; no ecdb, no on-disk database. --secret-cost 4 keeps bcrypt fast.
echo "starting in-memory server on :$PORT ..."
"$BIN/ec" serve --memory --listen ":$PORT" --dev --secret-cost 4 >"$WORK/ec.log" 2>&1 &
SERVER_PID=$!

base="http://localhost:$PORT/api"
ready=0
for _ in $(seq 1 50); do
	if curl -sf "$base/healthz" >/dev/null 2>&1; then
		ready=1
		break
	fi
	sleep 0.2
done
if [ "$ready" -ne 1 ]; then
	echo "server failed to start on :$PORT:" >&2
	cat "$WORK/ec.log" >&2
	exit 1
fi

# earl reads these; godotenv does not override already-set variables, so these
# win over any .env* the selected EARL_ENV loads.
export EARL_BASE_URL="$base"
export EARL_EMAIL="$ADMIN_EMAIL"
export EARL_SECRET="$ADMIN_SECRET"
earl="$BIN/earl"

pass=0
fail=0
step() { printf '\n\033[1m# %s\033[0m\n' "$*"; }
# ok NAME CMD...   — CMD must succeed
ok() {
	step "$1"
	shift
	if "$@"; then pass=$((pass + 1)); else
		echo "  !! UNEXPECTED FAILURE"
		fail=$((fail + 1))
	fi
}
# deny NAME CMD... — CMD must fail (e.g. a 401/403)
deny() {
	step "$1"
	shift
	if "$@"; then
		echo "  !! EXPECTED FAILURE BUT SUCCEEDED"
		fail=$((fail + 1))
	else pass=$((pass + 1)); fi
}

# A fresh in-memory database numbers the seeded admin=account 1, tester=account 2,
# and the first game and member from 1, which the paths below rely on.
ok "public health check" "$earl" get /healthz
ok "public version" "$earl" get /version
ok "login as well-known admin" "$earl" login
ok "whoami" "$earl" whoami
ok "create a user (tester)" "$earl" post /accounts -d '{"email":"tester@example.com","secret":"hunter2hunter2","isActive":true,"isAdmin":false}'
ok "list accounts" "$earl" get /accounts
ok "get account 2" "$earl" get /accounts/2
ok "update account 2 display name" "$earl" patch /accounts/2 -d '{"displayName":"Tester Two"}'
ok "create a game" "$earl" post /games -d '{"name":"Smoke Game","description":"created by scripts/smoke.sh"}'
ok "list games" "$earl" get /games
ok "get game 1" "$earl" get /games/1
ok "update game 1 description" "$earl" patch /games/1 -d '{"description":"updated by earl"}'
ok "add tester as GM of game 1" "$earl" post /games/1/members -d '{"accountId":2,"isGm":true}'
ok "list game 1 members" "$earl" get /games/1/members
ok "my sessions" "$earl" get /me/sessions
ok "my games (admin: none expected)" "$earl" get /me/games

ok "impersonate tester (account 2)" "$earl" impersonate 2
ok "as tester: whoami" "$earl" --email tester@example.com whoami
ok "as tester: my games (GM of game 1)" "$earl" --email tester@example.com get /me/games
deny "as tester: list accounts (403)" "$earl" --email tester@example.com get /accounts
ok "as tester: update game 1 as GM" "$earl" --email tester@example.com patch /games/1 -d '{"description":"edited by GM tester"}'

deny "unauthenticated /me (401)" "$earl" get /me --no-auth
ok "logout tester" "$earl" --email tester@example.com logout
ok "logout admin" "$earl" logout

printf '\n\033[1m==== %d passed, %d failed ====\033[0m\n' "$pass" "$fail"
exit "$fail"
