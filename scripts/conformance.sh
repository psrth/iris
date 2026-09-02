#!/usr/bin/env bash
# Exercises the iris relay contract end to end with curl. Doubles as a demo.
#
#   iris serve &            # in another terminal
#   scripts/conformance.sh  # defaults to http://127.0.0.1:7433
set -uo pipefail

ORIGIN=${1:-http://127.0.0.1:7433}
for tool in curl jq; do
	command -v "$tool" >/dev/null || { echo "need $tool" >&2; exit 1; }
done
TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
pass=0; fail=0

# req METHOD PATH [curl args...] -> sets CODE and BODY
req() {
	CODE=$(curl -sS -o "$TMP/body" -w '%{http_code}' -X "$1" "$ORIGIN$2" "${@:3}")
	BODY=$(cat "$TMP/body")
}
# check DESCRIPTION EXPECTED_CODE [jq filter that must be true]
check() {
	local ok=1
	[[ $CODE == "$2" ]] || ok=0
	[[ -z ${3:-} ]] || [[ $(jq -r "$3" <<<"$BODY" 2>/dev/null) == true ]] || ok=0
	if (( ok )); then pass=$((pass+1)); printf 'ok    %-44s %s\n' "$1" "$CODE"
	else fail=$((fail+1)); printf 'FAIL  %-44s %s %s\n' "$1" "$CODE" "$BODY"; fi
}
msg() { # msg ROLE NAME CONTENT [METADATA]
	local m=${4:-'{}'}
	jq -nc --arg r "$1" --arg n "$2" --arg c "$3" --argjson m "$m" \
		'{message:{role:$r,name:$n,content:$c},metadata:$m}'
}

echo "# provisioning"
req POST /sessions
check "POST /sessions" 201 '.uid and .key and .url and .limits.max_messages'
SID=$(jq -r .uid <<<"$BODY"); KEY=$(jq -r .key <<<"$BODY"); S="/s/$SID"
AUTH=(-H "Authorization: Bearer $KEY")
JSON=(-H "Content-Type: application/json")

echo "# auth"
req GET "$S"
check "GET without key" 401 '.error.code == "unauthorized"'
req GET "$S" -H "Authorization: Bearer nope"
check "GET with wrong key" 401
req GET /s/doesnotexist "${AUTH[@]}"
check "GET unknown session" 404 '.error.code == "not_found"'

echo "# messages"
req POST "$S" "${AUTH[@]}" "${JSON[@]}" -d "$(msg assistant parth-claude 'repro confirmed')"
check "POST message" 201 '.seq == 1 and .ts and .message.name == "parth-claude"'
req POST "$S" "${AUTH[@]}" "${JSON[@]}" -d "$(msg system iris 'spoof')"
check "POST role=system rejected" 400 '.error.code == "invalid_request"'
req POST "$S" "${AUTH[@]}" "${JSON[@]}" -d '{"message":{"role":"user","name":"bad name","content":"x"}}'
check "POST bad handle rejected" 400
req POST "$S" "${AUTH[@]}" "${JSON[@]}" -d "$(msg user parth 'looks right, ship it' '{"attn":"human","reply_to":1}')"
check "POST message with metadata" 201 '.seq == 2 and .metadata.attn == "human"'
req GET "$S?since=0" "${AUTH[@]}"
check "GET since=0" 200 '(.messages | length) == 2 and .last_seq == 2'
req GET "$S?since=2" "${AUTH[@]}"
check "GET since=head is empty" 200 '(.messages | length) == 0 and .last_seq == 2'

echo "# long-poll"
req GET "$S/wait?since=2&timeout=0" "${AUTH[@]}"
check "wait timeout=0 with nothing new" 204
curl -sS -o "$TMP/wait" -w '%{http_code}' "$ORIGIN$S/wait?since=2&timeout=10" "${AUTH[@]}" >"$TMP/waitcode" &
sleep 0.3
req POST "$S" "${AUTH[@]}" "${JSON[@]}" -d "$(msg assistant other-agent 'need eyes on this' '{"urgent":true}')"
check "POST urgent message" 201 '.seq == 3'
wait
CODE=$(cat "$TMP/waitcode"); BODY=$(cat "$TMP/wait")
check "wait woke on append" 200 '.messages[0].seq == 3 and .last_seq == 3'
req GET "$S/wait?since=0&timeout=0&filter=urgent" "${AUTH[@]}"
check "wait filter=urgent returns matches only" 200 '(.messages | length) == 1 and .messages[0].seq == 3'

echo "# files"
printf 'trace line 1\ntrace line 2\n' >"$TMP/trace.txt"
req PUT "$S/files/trace.txt" "${AUTH[@]}" -H "Content-Type: text/plain" --data-binary @"$TMP/trace.txt"
check "PUT file" 201 '.seq == 4 and .metadata.event == "file_uploaded" and .metadata.file.size == 26'
req GET "$S/files" "${AUTH[@]}"
check "GET file list" 200 '.files[0].name == "trace.txt" and .files[0].seq == 4'
CODE=$(curl -sS -o "$TMP/dl" -w '%{http_code}' "$ORIGIN$S/files/trace.txt" "${AUTH[@]}"); BODY=""
cmp -s "$TMP/dl" "$TMP/trace.txt" || CODE="body-mismatch"
check "GET file bytes round-trip" 200
req GET "$S/files/missing" "${AUTH[@]}"
check "GET missing file" 404
req POST "$S" "${AUTH[@]}" "${JSON[@]}" -d '{"message":{"role":"assistant","name":"parth-claude","content":[{"type":"text","text":"attached"},{"type":"file","file":{"name":"trace.txt","seq":4}}]}}'
check "POST typed parts referencing file" 201 '.seq == 5'

echo "# lifecycle"
req POST "$S/terminate" "${AUTH[@]}"
check "POST terminate" 200 '.status == "read-only" and .purge_at'
req GET "$S?since=5" "${AUTH[@]}"
check "terminate appended system event" 200 '.messages[0].metadata.event == "session_terminated"'
req POST "$S" "${AUTH[@]}" "${JSON[@]}" -d "$(msg user parth 'too late')"
check "POST after terminate" 409 '.error.code == "session_read_only"'
req GET "$S?since=0" "${AUTH[@]}"
check "GET still works in grace" 200 '.last_seq == 6'

echo
echo "$pass passed, $fail failed"
(( fail == 0 ))
