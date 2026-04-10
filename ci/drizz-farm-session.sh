#!/bin/bash
# drizz-farm-session.sh — Generic CI helper for any CI system
#
# Usage:
#   source ci/drizz-farm-session.sh
#   drizz_acquire                # → sets $ADB_SERIAL, $APPIUM_URL, $SESSION_ID
#   drizz_record_start           # → starts video recording
#   <your test commands here>
#   drizz_record_stop            # → stops recording, pulls video
#   drizz_artifacts              # → downloads recording + logcat
#   drizz_release                # → releases device back to pool
#
# Environment:
#   FARM_URL  — drizz-farm API (default: http://localhost:9401)
#   PLATFORM  — android or ios (default: android)
#   CLIENT    — identifier for this CI job (default: ci-$$)

FARM_URL="${FARM_URL:-http://localhost:9401}"
PLATFORM="${PLATFORM:-android}"
CLIENT="${CLIENT:-ci-$$}"

drizz_acquire() {
  echo "[drizz-farm] Acquiring device..."
  local RESPONSE
  RESPONSE=$(curl -sf -X POST "$FARM_URL/api/v1/sessions" \
    -H "Content-Type: application/json" \
    --max-time 300 \
    -d "{\"platform\":\"$PLATFORM\",\"source\":\"ci\",\"client_name\":\"$CLIENT\"}")

  if [ -z "$RESPONSE" ]; then
    echo "[drizz-farm] ERROR: Failed to acquire device"
    return 1
  fi

  export SESSION_ID=$(echo "$RESPONSE" | jq -r '.id')
  export ADB_SERIAL=$(echo "$RESPONSE" | jq -r '.connection.adb_serial')
  export ADB_HOST=$(echo "$RESPONSE" | jq -r '.connection.host')
  export ADB_PORT=$(echo "$RESPONSE" | jq -r '.connection.adb_port')
  export APPIUM_URL=$(echo "$RESPONSE" | jq -r '.connection.appium_url')
  export NODE_NAME=$(echo "$RESPONSE" | jq -r '.node_name')
  export CONSOLE_PORT=$(echo "$RESPONSE" | jq -r '.connection.console_port')

  echo "[drizz-farm] Device: $ADB_SERIAL on $NODE_NAME (session: $SESSION_ID)"
  echo "[drizz-farm] Appium: $APPIUM_URL"
  echo "[drizz-farm] ADB:    adb -s $ADB_SERIAL"
}

drizz_record_start() {
  [ -z "$SESSION_ID" ] && { echo "[drizz-farm] No session"; return 1; }
  curl -sf -X POST "$FARM_URL/api/v1/sessions/$SESSION_ID/recording/start" >/dev/null 2>&1 && \
    echo "[drizz-farm] Recording started" || echo "[drizz-farm] Recording not available"
}

drizz_record_stop() {
  [ -z "$SESSION_ID" ] && return
  curl -sf -X POST "$FARM_URL/api/v1/sessions/$SESSION_ID/recording/stop" >/dev/null 2>&1
  echo "[drizz-farm] Recording stopped"
}

drizz_screenshot() {
  [ -z "$SESSION_ID" ] && return
  curl -sf -X POST "$FARM_URL/api/v1/sessions/$SESSION_ID/screenshot" >/dev/null 2>&1
  echo "[drizz-farm] Screenshot taken"
}

drizz_artifacts() {
  [ -z "$SESSION_ID" ] && return
  local DIR="${ARTIFACTS_DIR:-.}"
  mkdir -p "$DIR"

  curl -sf "$FARM_URL/api/v1/sessions/$SESSION_ID/recording/download" -o "$DIR/recording.mp4" 2>/dev/null && \
    echo "[drizz-farm] Video: $DIR/recording.mp4" || true
  curl -sf "$FARM_URL/api/v1/sessions/$SESSION_ID/logcat/download" -o "$DIR/logcat.txt" 2>/dev/null && \
    echo "[drizz-farm] Logcat: $DIR/logcat.txt" || true
}

drizz_release() {
  [ -z "$SESSION_ID" ] && return
  curl -sf -X DELETE "$FARM_URL/api/v1/sessions/$SESSION_ID" >/dev/null 2>&1
  echo "[drizz-farm] Device released (session: $SESSION_ID)"
  unset SESSION_ID ADB_SERIAL ADB_HOST ADB_PORT APPIUM_URL NODE_NAME
}

# Trap to release device on script exit (safety net)
_drizz_cleanup() {
  if [ -n "$SESSION_ID" ]; then
    echo "[drizz-farm] Cleanup: releasing device on exit..."
    drizz_release
  fi
}
trap _drizz_cleanup EXIT
