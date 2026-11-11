#!/usr/bin/env bash
# End-to-end smoke flow against a running airval: login, API key, target -> dataset (CSV import) -> suite + gate,
# then `airval run --gate --markdown` twice (passing gate, deliberately failing gate).
#
#   AIRVAL_URL=http://localhost:8080 MOCK_URL=http://mocktarget:9090 \
#     ADMIN_EMAIL=admin@example.com ADMIN_PASSWORD=... N=1000 deploy/verify/flow.sh
#
# MOCK_URL is the address of the mock target as seen by the *workers*. AIRVAL_BIN defaults to ./bin/airval.
# Needs bash, curl and python3.
set -euo pipefail
URL=${AIRVAL_URL:-http://localhost:8080}
MOCK_URL=${MOCK_URL:-http://127.0.0.1:9090}
N=${N:-50}
EMAIL=${ADMIN_EMAIL:-admin@example.com}
PASS=${ADMIN_PASSWORD:?set ADMIN_PASSWORD}
BIN=${AIRVAL_BIN:-./bin/airval}
TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
J="$TMP/cookies"
CSRF=""

api() { # method path [json-body]  -> body on stdout, fails on >=400
  local m=$1 p=$2; shift 2
  local args=(-sS -b "$J" -c "$J" -H "X-CSRF-Token: $CSRF" -H 'Content-Type: application/json' -X "$m" -w '\n%{http_code}')
  [ $# -gt 0 ] && args+=(--data-binary "$1")
  local out code; out=$(curl "${args[@]}" "$URL$p"); code=${out##*$'\n'}; out=${out%$'\n'*}
  if [ "$code" -ge 400 ]; then echo "HTTP $code $m $p: $out" >&2; return 1; fi
  printf '%s' "$out"
}
jqr() { python3 -c 'import json,sys; d=json.load(sys.stdin); print(eval(sys.argv[1]))' "$1"; }

# 1. auth (login; first-run setup when nothing exists yet)
if [ "$(curl -fsS "$URL/v1/setup" | jqr 'd.get("needs_setup", d.get("setup_required", False))')" = "True" ]; then
  api POST /v1/setup "{\"email\":\"$EMAIL\",\"password\":\"$PASS\",\"org_name\":\"Flow\"}" >/dev/null
fi
ME=$(curl -fsS -c "$J" -H 'Content-Type: application/json' -d "{\"email\":\"$EMAIL\",\"password\":\"$PASS\"}" "$URL/v1/auth/login")
CSRF=$(echo "$ME" | jqr 'd["csrf_token"]'); ORG=$(echo "$ME" | jqr 'd["orgs"][0]["id"]')
KEY=$(api POST "/v1/orgs/$ORG/api-keys" '{"name":"flow-ci","role":"admin"}' | jqr 'd["key"]')
echo "org=$ORG key=${KEY:0:9}..."

# 2. project, target (mock RAG), dataset (CSV import), evaluators, suite
PROJECT=$(api POST /v1/projects "{\"org_id\":\"$ORG\",\"name\":\"Flow $(date +%s)\"}" | jqr 'd["id"]')
TARGET_BODY=$(python3 -c '
import json,sys
print(json.dumps({"name":"mock rag","kind":"http","config":{"url":sys.argv[1]+"/rag",
 "body":"{\"question\": {{ json .input.question }}, \"expected\": {{ json .example.expected }}}",
 "response":{"output":"$.answer","context":"$.context[*].text","input_tokens":"$.usage.input_tokens","output_tokens":"$.usage.output_tokens"}}}))' "$MOCK_URL")
TARGET=$(api POST "/v1/projects/$PROJECT/targets" "$TARGET_BODY" | jqr 'd["id"]')
DATASET=$(api POST "/v1/projects/$PROJECT/datasets" '{"name":"facts"}' | jqr 'd["id"]')
IMPORT_BODY=$(python3 -c '
import csv,io,json,sys
n=int(sys.argv[1])
def fnv64a(s):  # the mock target derives its ground truth from fnv64a(question)
    h=0xcbf29ce484222325
    for b in s.encode(): h=((h^b)*0x100000001b3)&0xFFFFFFFFFFFFFFFF
    return h
buf=io.StringIO(); w=csv.writer(buf); w.writerow(["question","expected"])
for i in range(n):
    q="What is fact number %d?"%i; w.writerow([q,"The answer is %08x."%(fnv64a(q)&0xFFFFFFFF)])
print(json.dumps({"source":{"kind":"inline","format":"csv","data":buf.getvalue()},
  "mapping":{"input_columns":{"question":"question"},"expected_column":"expected"},"publish":True}))' "$N")
api POST "/v1/datasets/$DATASET:import" "$IMPORT_BODY" | head -c 300; echo
mkev() { api POST "/v1/projects/$PROJECT/evaluators" "{\"name\":\"$1\",\"kind\":\"$2\",\"config\":$3}" | jqr 'd["id"]'; }
EV_EQ=$(mkev exact_match equals '{}')
EV_FMT=$(mkev format regex '{"pattern":"^The answer is [0-9a-f]{8}\\.$"}')
EV_LAT=$(mkev latency latency_ms '{"max_ms":10000}')
SUITE=$(api POST "/v1/projects/$PROJECT/suites" "{\"name\":\"facts suite\",\"config\":{\"target_ids\":[\"$TARGET\"],\"datasets\":[{\"dataset_id\":\"$DATASET\"}],\"evaluators\":[{\"evaluator_id\":\"$EV_EQ\",\"severity\":\"critical\"},{\"evaluator_id\":\"$EV_FMT\"},{\"evaluator_id\":\"$EV_LAT\",\"severity\":\"minor\"}]}}" | jqr 'd["id"]')
gate() { api POST "/v1/suites/$SUITE/gates" "{\"name\":\"$1\",\"expr\":{\"op\":\"cmp\",\"cmp\":\">=\",\"args\":[{\"op\":\"call\",\"func\":\"mean\",\"params\":[\"$EV_EQ\"]},{\"op\":\"num\",\"value\":$2}]}}" | jqr 'd["id"]'; }
G1=$(gate "exact match >= 0.5" 0.5)

# 3. run with a passing gate
export AIRVAL_URL=$URL AIRVAL_API_KEY=$KEY
echo "== run 1 (gate mean(exact_match) >= 0.5, mock accuracy 0.8): expect exit 0"
S=$(date +%s); rc=0; "$BIN" run --suite "$SUITE" --gate --no-progress --markdown "$TMP/r1.md" || rc=$?
E=$(date +%s); echo "exit=$rc elapsed=$((E-S))s examples=$N"; sed -n 1,12p "$TMP/r1.md"
[ "$rc" = 0 ] || { echo "FAIL: expected exit 0"; exit 1; }

# 4. deliberately failing gate
api DELETE "/v1/suites/$SUITE/gates/$G1" >/dev/null
gate "exact match >= 0.999" 0.999 >/dev/null
echo "== run 2 (gate >= 0.999): expect exit 1"
rc=0; "$BIN" run --suite "$SUITE" --gate --no-progress --markdown "$TMP/r2.md" || rc=$?
echo "exit=$rc"; sed -n 1,12p "$TMP/r2.md"
[ "$rc" = 1 ] || { echo "FAIL: expected exit 1"; exit 1; }
echo "FLOW OK"
