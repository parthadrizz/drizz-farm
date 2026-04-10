#!/bin/bash
# run-test.sh — Run tests on drizz-farm emulators
# Uses drizz-farm API (localhost:9401) for device management
# Uses stag.drizz.dev for Drizz AI test execution
# Usage: ./scripts/run-test.sh [test-file.txt]

set -euo pipefail

FARM_URL="http://localhost:9401/api/v1"
DRIZZ_URL="https://stag.drizz.dev/api/desktop"
TM_URL="https://stag.drizz.dev/api/tm/desktop"

# Auth token from keychain
TOKEN=$(security find-generic-password -s "drizz-auth" -a "humblefool2909:access" -w 2>/dev/null)
[ -z "$TOKEN" ] && { echo "ERROR: No auth token. Run desktop app to login."; exit 1; }
echo "✓ Auth token loaded"

# Step 1: Check farm is running
POOL=$(curl -sf "$FARM_URL/pool" 2>/dev/null) || { echo "ERROR: drizz-farm not running on :9401"; exit 1; }
echo "✓ drizz-farm is running"

# Step 2: Get a device from the farm (boot on-demand if needed)
INSTANCES=$(echo "$POOL" | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d.get('instances',[])))" 2>/dev/null)
if [ "$INSTANCES" = "0" ]; then
  echo "→ No warm devices, booting one on-demand..."
  curl -sf -X POST "$FARM_URL/pool/boot" >/dev/null 2>&1 || true
  sleep 8
  POOL=$(curl -sf "$FARM_URL/pool")
fi

# Get first available device from farm
DEVICE_INFO=$(echo "$POOL" | python3 -c "
import sys, json
d = json.load(sys.stdin)
for inst in d.get('instances', []):
    if inst.get('serial'):
        print(json.dumps({'serial': inst['serial'], 'id': inst['id'], 'name': inst.get('device_name', 'emulator'), 'session_id': inst.get('session_id', inst['id'])}))
        break
" 2>/dev/null)

[ -z "$DEVICE_INFO" ] && { echo "ERROR: No devices available in farm"; exit 1; }

SERIAL=$(echo "$DEVICE_INFO" | python3 -c "import sys,json; print(json.load(sys.stdin)['serial'])")
DEVICE_NAME=$(echo "$DEVICE_INFO" | python3 -c "import sys,json; print(json.load(sys.stdin)['name'])")
FARM_SESSION=$(echo "$DEVICE_INFO" | python3 -c "import sys,json; print(json.load(sys.stdin)['session_id'])")
echo "✓ Device: $DEVICE_NAME ($SERIAL) — farm session: $FARM_SESSION"

# Step 3: Read test commands
if [ -n "${1:-}" ] && [ -f "$1" ]; then
  COMMANDS=$(cat "$1")
elif [ ! -t 0 ]; then
  COMMANDS=$(cat)
else
  COMMANDS="OPEN_APP: com.google.android.youtube
Type \"cute panda\" in search bar
Tap on the first video"
fi
CMD_COUNT=$(echo "$COMMANDS" | grep -c '.' || true)
echo "✓ Test: $CMD_COUNT commands"
echo "┌──────────────────────────────────"
echo "$COMMANDS" | sed 's/^/│ /'
echo "└──────────────────────────────────"

# Step 4: Get org ID from Drizz backend
ORG_ID=$(curl -sf -H "Authorization: Bearer $TOKEN" "$TM_URL/api/organizations/me" 2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('id', d.get('data',{}).get('id','default')))" 2>/dev/null || echo "default")
echo "✓ Org: $ORG_ID"

# Step 5: Create execution session on Drizz backend
echo ""
echo "→ Creating Drizz session..."
SESSION_RESPONSE=$(curl -sf -X POST "$DRIZZ_URL/v1/sessions" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"user_id\": \"partha@drizz.dev\",
    \"device_id\": \"$SERIAL\",
    \"device_name\": \"$DEVICE_NAME\",
    \"provider\": \"LOCAL_CLIENT\",
    \"configuration\": { \"platform\": \"ANDROID\" }
  }")

DRIZZ_SESSION=$(echo "$SESSION_RESPONSE" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('data',{}).get('session_id',''))" 2>/dev/null || echo "")
WS_URL=$(echo "$SESSION_RESPONSE" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('data',{}).get('websocket_url',''))" 2>/dev/null || echo "")
WS_TOKEN=$(echo "$SESSION_RESPONSE" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('data',{}).get('authentication_token',''))" 2>/dev/null || echo "")

[ -z "$DRIZZ_SESSION" ] && { echo "ERROR: Failed to create Drizz session"; echo "$SESSION_RESPONSE"; exit 1; }
echo "✓ Drizz session: $DRIZZ_SESSION"

# Step 6: Start video recording + logcat on farm
echo "→ Starting recording..."
curl -sf -X POST "$FARM_URL/sessions/$FARM_SESSION/recording/start" >/dev/null 2>&1 && echo "✓ Video recording started" || echo "  (recording not available)"

# Step 7: Submit test to Drizz AI engine
echo "→ Submitting test to Drizz AI..."
TMPFILE=$(mktemp /tmp/drizz-test-XXXXXX.txt)
echo "$COMMANDS" > "$TMPFILE"

# Extract app name from OPEN_APP command if present
APP_NAME=$(echo "$COMMANDS" | grep -i "OPEN_APP" | head -1 | sed 's/.*OPEN_APP:\s*//' | tr -d '\r' || echo "com.google.android.youtube")

EXEC_RESPONSE=$(curl -sf -X POST "$DRIZZ_URL/v1/executions" \
  -H "Authorization: Bearer $TOKEN" \
  -F "session_id=$DRIZZ_SESSION" \
  -F "device_id=$SERIAL" \
  -F "device_name=$DEVICE_NAME" \
  -F "provider=LOCAL_CLIENT" \
  -F "app_name=$APP_NAME" \
  -F "org_id=$ORG_ID" \
  -F "platform=ANDROID" \
  -F "app_state_control={\"kill_app_before\":false,\"kill_app_after\":false}" \
  -F "content=@$TMPFILE;type=text/plain;filename=test.txt")
rm -f "$TMPFILE"

THREAD_CODE=$(echo "$EXEC_RESPONSE" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('data',{}).get('request_id',''))" 2>/dev/null || echo "")
EXEC_ID=$(echo "$EXEC_RESPONSE" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('data',{}).get('execution_id',''))" 2>/dev/null || echo "")

[ -z "$THREAD_CODE" ] && { echo "ERROR: Test submission failed"; echo "$EXEC_RESPONSE"; exit 1; }
echo "✓ Executing: $EXEC_ID (thread: $THREAD_CODE)"

# Step 7: Stream logs via WebSocket
echo ""
echo "════════════════════════════════════"
echo "  LIVE TEST OUTPUT"
echo "════════════════════════════════════"

if [ -n "$WS_URL" ] && [ -n "$WS_TOKEN" ]; then
  if echo "$WS_URL" | grep -q "^ws"; then
    FULL_WS_URL="${WS_URL}?token=${WS_TOKEN}"
  else
    FULL_WS_URL="wss://${WS_URL}?token=${WS_TOKEN}"
  fi

  if command -v websocat &>/dev/null; then
    websocat "$FULL_WS_URL" 2>/dev/null | while IFS= read -r line; do
      echo "$line" | python3 -c "
import sys,json
try:
  d=json.load(sys.stdin)
  event=d.get('event','')
  msg=d.get('message','')
  color=d.get('color','')
  typ=d.get('type','')
  if event=='log':
    prefix = '  ✓' if color=='green' else '  ✗' if color=='red' else '  →'
    print(f'{prefix} {msg}')
    if typ in ('WORKFLOW_COMPLETED','EXECUTION_COMPLETE','EXECUTION_FAILED','WORKFLOW_CANCELLED'):
      status = 'PASSED' if 'COMPLETE' in typ else 'FAILED'
      print(f'\n  ══ TEST {status} ══')
      sys.exit(0)
  elif msg:
    print(f'  [{event}] {msg}')
except Exception as e:
  pass
" 2>/dev/null && break
    done
  else
    echo "  (install websocat for live log streaming: brew install websocat)"
    echo "  Waiting 60s for test to complete..."
    sleep 60
  fi
fi

# Cleanup — stop recording, save artifacts, delete sessions
echo ""
echo "→ Saving artifacts..."

# Stop recording and download video
curl -sf -X POST "$FARM_URL/sessions/$FARM_SESSION/recording/stop" >/dev/null 2>&1 || true
RECORDING_DIR="$HOME/.drizz-farm/recordings/$FARM_SESSION"
mkdir -p "$RECORDING_DIR"
curl -sf "$FARM_URL/sessions/$FARM_SESSION/recording/download" -o "$RECORDING_DIR/test-recording.mp4" 2>/dev/null && \
  echo "✓ Video saved: $RECORDING_DIR/test-recording.mp4" || echo "  (no video)"

# Save screenshot
curl -sf -X POST "$FARM_URL/sessions/$FARM_SESSION/screenshot" -o "$RECORDING_DIR/final-screenshot.png" 2>/dev/null && \
  echo "✓ Screenshot saved: $RECORDING_DIR/final-screenshot.png" || echo "  (no screenshot)"

# Save logcat
curl -sf "$FARM_URL/sessions/$FARM_SESSION/logcat/download" -o "$RECORDING_DIR/logcat.txt" 2>/dev/null && \
  echo "✓ Logcat saved: $RECORDING_DIR/logcat.txt" || echo "  (no logcat)"

echo ""
echo "→ Cleaning up sessions..."
curl -sf -X POST "$DRIZZ_URL/v1/sessions/$DRIZZ_SESSION" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"session_id\":\"$DRIZZ_SESSION\",\"user_id\":\"partha@drizz.dev\",\"device_id\":\"$SERIAL\"}" >/dev/null 2>&1 || true
echo "✓ Done — artifacts in $RECORDING_DIR"
