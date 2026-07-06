#!/usr/bin/env sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "Please run as root or through sudo." >&2
  exit 1
fi

if command -v systemctl >/dev/null 2>&1; then
  systemctl disable --now almysama-agent 2>/dev/null || true
  rm -f /etc/systemd/system/almysama-agent.service
  systemctl daemon-reload 2>/dev/null || true
fi

if command -v rc-service >/dev/null 2>&1; then
  rc-service almysama-agent stop 2>/dev/null || true
  rc-update del almysama-agent default 2>/dev/null || true
  rm -f /etc/init.d/almysama-agent
fi

rm -f /usr/local/bin/almysama-agent
rm -rf /etc/almysama-agent

echo "Almysama Agent removed."
