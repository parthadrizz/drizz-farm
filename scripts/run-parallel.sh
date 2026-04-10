#!/bin/bash
# run-parallel.sh — 5 concurrent tests, each opens 5 system apps with 5s waits
# Records video of each test. 3 run immediately, 2 queue.

set -euo pipefail

FARM="http://localhost:9401/api/v1"
ADB=~/Library/Android/sdk/platform-tools/adb

curl -sf "$FARM/pool" >/dev/null 2>&1 || { echo "ERROR: drizz-farm not running"; exit 1; }
echo "✓ Farm is live"

# Each test opens these 5 apps in order with 5s between each
APPS=("com.google.android.gm" "com.google.android.apps.maps" "com.google.android.googlequicksearchbox" "com.google.android.youtube" "com.google.android.contacts")
APP_NAMES=("Gmail" "Maps" "Google" "YouTube" "Contacts")

# 4 test labels (3 run, 1 queues)
TAGS=("TEST-1" "TEST-2" "TEST-3" "TEST-4")
COLORS=("\033[36m" "\033[33m" "\033[35m" "\033[32m")
RESET="\033[0m"

run_test() {
  local IDX=$1
  local COLOR="${COLORS[$IDX]}"
  local TAG="${TAGS[$IDX]}"
  local T0=$(date +%s)

  echo -e "${COLOR}[$TAG] → Requesting device...${RESET}"

  # 1. Create session (boots on-demand, queues if at capacity)
  local RESP=$(curl -s -X POST "$FARM/sessions" \
    -H "Content-Type: application/json" \
    --max-time 300 \
    -d "{\"platform\":\"android\",\"source\":\"test-runner\",\"client_name\":\"$TAG\"}")

  local SID=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")
  local SERIAL=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('connection',{}).get('adb_serial',''))" 2>/dev/null || echo "")

  if [ -z "$SERIAL" ]; then
    echo -e "${COLOR}[$TAG] ✗ No device allocated${RESET}"
    return 1
  fi
  echo -e "${COLOR}[$TAG] ✓ Got $SERIAL ($(( $(date +%s) - T0 ))s wait)${RESET}"

  # 2. Start video recording
  curl -sf -X POST "$FARM/sessions/$SID/recording/start" >/dev/null 2>&1 && \
    echo -e "${COLOR}[$TAG] 🔴 Recording${RESET}" || true

  # 3. Open 5 apps, 5 seconds each
  for i in 0 1 2 3 4; do
    local APP="${APPS[$i]}"
    local NAME="${APP_NAMES[$i]}"

    echo -e "${COLOR}[$TAG] → [$((i+1))/5] Opening $NAME...${RESET}"
    $ADB -s "$SERIAL" shell am start -a android.intent.action.MAIN -c android.intent.category.LAUNCHER -n "$($ADB -s "$SERIAL" shell cmd package resolve-activity --brief "$APP" 2>/dev/null | tail -1)" >/dev/null 2>&1 \
      || $ADB -s "$SERIAL" shell monkey -p "$APP" -c android.intent.category.LAUNCHER 1 >/dev/null 2>&1

    sleep 2

    # Quick scroll to show interaction
    $ADB -s "$SERIAL" shell input swipe 540 1500 540 700 600
    sleep 3

    # Verify what's in foreground
    local FOCUS=$($ADB -s "$SERIAL" shell "dumpsys window | grep mCurrentFocus" 2>/dev/null | sed 's/.*u0 //' | sed 's/}.*//')
    echo -e "${COLOR}[$TAG]   ✓ $NAME — focus: $FOCUS${RESET}"
  done

  # 4. Screenshot final state
  curl -sf -X POST "$FARM/sessions/$SID/screenshot" >/dev/null 2>&1 && \
    echo -e "${COLOR}[$TAG] 📸 Screenshot saved${RESET}" || true

  # 5. Stop recording
  local STOP_RESP=$(curl -s -X POST "$FARM/sessions/$SID/recording/stop" 2>/dev/null)
  local REC_FILE=$(echo "$STOP_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('file',''))" 2>/dev/null || echo "")
  [ -n "$REC_FILE" ] && echo -e "${COLOR}[$TAG] 📹 Video: $REC_FILE${RESET}"

  # 6. Release session — device goes back to pool for queued tests
  curl -sf -X DELETE "$FARM/sessions/$SID" >/dev/null 2>&1

  local TOTAL=$(( $(date +%s) - T0 ))
  echo -e "${COLOR}[$TAG] ══ DONE on $SERIAL — ${TOTAL}s total ══${RESET}"
}

echo ""
echo "════════════════════════════════════════════════════════"
echo "  5 tests × 5 apps each (Gmail→Maps→Google→YouTube→Contacts)"
echo "  max 3 concurrent · 2 will queue · video recorded"
echo "════════════════════════════════════════════════════════"
echo ""

for i in 0 1 2 3; do
  run_test $i &
  sleep 0.5
done

echo "→ All 5 launched"
echo ""
wait

echo ""
echo "════════════════════════════════════════════════════════"
echo "  ALL 4 COMPLETE"
echo "════════════════════════════════════════════════════════"
echo ""
echo "📹 Recordings:"
find ~/.drizz-farm/artifacts -name "*.mp4" -mmin -10 2>/dev/null | while read f; do
  SIZE=$(ls -lh "$f" | awk '{print $5}')
  echo "  $f ($SIZE)"
done
echo ""
curl -s "$FARM/pool" | python3 -c "
import sys,json
d=json.load(sys.stdin)
insts=d.get('instances',[])
print(f'Pool: {len(insts)} devices — {d[\"warm\"]} warm, {d[\"allocated\"]} allocated')
for i in insts:
    print(f'  {i[\"serial\"]:16s} {i[\"state\"]:10s} {i.get(\"device_name\",\"\")}')
" 2>/dev/null
echo ""
echo "Dashboard: http://localhost:9401"
