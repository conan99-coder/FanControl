#!/usr/bin/env bash
# FanControl installer (run ON THE RIG, in /tmp).
#
# Idempotent: safe to re-run. This script ONLY installs the binary + a dry-run
# configuration and registers the service as DISABLED (does not auto-start). It
# does NOT enable fan writes.
set -euo pipefail

APP=/usr/local/bin/fanctrl
ETC=/etc/fanctrl
LIB=/var/lib/fanctrl
SRC=/tmp/fanctrl-linux-amd64

echo "==> Installing binary to ${APP}"
sudo install -m 0755 "$SRC" "$APP"

echo "==> Creating directories"
sudo mkdir -p "$ETC" "$LIB"

echo "==> Installing service unit (start-at-boot, but NOT enabled/started yet)"
sudo install -m 0644 /tmp/fanctrl.service /etc/systemd/system/fanctrl.service
sudo systemctl daemon-reload

# Install the dry-run config unless one already exists (never clobber a config
# the operator may have edited).
if [ ! -f "$ETC/config.yaml" ]; then
  echo "==> Writing dry-run config to ${ETC}/config.yaml"
  sudo install -m 0644 /tmp/fanctrl.live.yaml "$ETC/config.yaml"
else
  echo "==> ${ETC}/config.yaml already exists; leaving it unchanged."
fi

# BMC password placeholder (0600). User must fill the real value.
BMC_PW="$ETC/bmc_password"
if [ ! -s "$BMC_PW" ]; then
  echo "==> Creating ${BMC_PW} placeholder. EDIT THIS with your BMC password."
  echo -n "REPLACE_WITH_BMC_PASSWORD" | sudo tee "$BMC_PW" >/dev/null
  sudo chmod 600 "$BMC_PW"
else
  echo "==> ${BMC_PW} already set; leaving unchanged."
fi

# Session signing secret (0600, random). The redirection must run as root, so
# it lives inside the sudo sh -c rather than outside it.
if [ ! -s "$ETC/secret" ]; then
  echo "==> Generating session secret at ${ETC}/secret"
  sudo sh -c "head -c 32 /dev/urandom | base64 > '${ETC}/secret'"
  sudo chmod 600 "$ETC/secret"
else
  echo "==> ${ETC}/secret already set; leaving unchanged."
fi

echo ""
echo "==> DONE. Not yet started. To validate in dry-run (no writes):"
echo "    sudo ${APP} --config ${ETC}/config.yaml"
echo ""
echo "    Then in another terminal:"
echo "      curl -s http://127.0.0.1:8080/api/metrics"
echo "      curl -s http://127.0.0.1:8080/api/discovery"
echo "      curl -s http://127.0.0.1:8080/api/status"
echo ""
echo "    NOTE: before validating, edit ${BMC_PW} to set your real BMC password."
