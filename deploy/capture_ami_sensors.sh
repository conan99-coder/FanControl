#!/usr/bin/env bash
# Capture the complete AMI sensor readings (temps, fans, voltages) from the
# Gigabyte BMC. Read-only. Runs on the rig, which can reach the BMC LAN.
#
# Usage: BMC_IP=<bmc-ip> BMC_USER=admin BMC_PASSWORD=<pw> ./capture_ami_sensors.sh
set -euo pipefail

BMC_IP="${BMC_IP:?set BMC_IP env var}"
BMC_USER="${BMC_USER:-admin}"
BMC_PASSWORD="${BMC_PASSWORD:?set BMC_PASSWORD env var}"

COOKIE=$(mktemp)
trap 'rm -f "$COOKIE"' EXIT

# 1) Seed anonymous session
curl -k -s -c "$COOKIE" "https://${BMC_IP}/" -o /dev/null

# 2) Login, capture CSRF token from the JSON response (password via env var,
#    never embedded in this script or the shell history)
LOGIN=$(curl -k -s -b "$COOKIE" -c "$COOKIE" -X POST "https://${BMC_IP}/api/session" \
  -H 'Content-Type: application/x-www-form-urlencoded; charset=UTF-8' \
  --data-urlencode "username=${BMC_USER}" \
  --data-urlencode "password=${BMC_PASSWORD}")
CSRF=$(printf '%s' "$LOGIN" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("CSRFToken",""))')

if [ -z "$CSRF" ]; then
  echo "ERROR: login failed; response: $LOGIN" >&2
  exit 1
fi

# 3) Full sensor readings
curl -k -s -b "$COOKIE" "https://${BMC_IP}/api/detail_sensors_readings" \
  -H "X-CSRFTOKEN: $CSRF"
