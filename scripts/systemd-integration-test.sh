#!/usr/bin/env bash
#
# systemd-integration-test.sh — run the pinner managed-service integration test
# against a REAL systemd user manager inside a systemd-as-PID1 container.
#
# This is the regression protection for "mcp install --service actually runs":
# it installs the pinner-mcp managed service as a systemd USER unit, starts it,
# confirms systemctl --user reports it active, and confirms the underlying
# pinner MCP http server comes up OAuth-protected and serves the discovery
# endpoint. Every prior systemd test in this repo used command/fs fakes; this is
# the first to exercise the real init-system lifecycle.
#
# Run it inside a privileged systemd container (systemd is PID 1):
#
#   docker run --rm --privileged \
#     -v /sys/fs/cgroup:/sys/fs/cgroup:rw \
#     -v "$PWD":/src -w /src \
#     jrei/systemd-ubuntu:22.04 \
#     ./scripts/systemd-integration-test.sh
#
# Or locally on a systemd host. Requires Go + the compiled pinner binary.
set -euo pipefail

echo "==> verifying systemd is running (must be PID 1)"
if [ "$(cat /proc/1/comm)" != "systemd" ]; then
  echo "SKIP: not inside a systemd container (PID 1 = $(cat /proc/1/comm))" >&2
  exit 0
fi
systemctl is-system-running >/dev/null 2>&1 || true

# The test runs as the current user; systemd USER services need a user manager.
# Enable that and bring it up on an explicit runtime dir + session D-Bus bus.
TEST_USER="$(id -un)"
TEST_UID="$(id -u)"
RUNTIME_DIR="/run/user/${TEST_UID}"

echo "==> enabling linger for ${TEST_USER} so the user manager runs without a login"
loginctl enable-linger "${TEST_USER}" >/dev/null 2>&1 || true
install -d -m 700 "${RUNTIME_DIR}"

echo "==> starting session D-Bus + systemd --user"
if [ ! -S "${RUNTIME_DIR}/bus" ]; then
  dbus-daemon --session --address="unix:path=${RUNTIME_DIR}/bus" --fork
fi
export XDG_RUNTIME_DIR="${RUNTIME_DIR}"
export DBUS_SESSION_BUS_ADDRESS="unix:path=${RUNTIME_DIR}/bus"
if ! pgrep -u "${TEST_UID}" -f "systemd --user" >/dev/null; then
  systemd --user >/dev/null 2>&1 &
  disown || true
fi
for _ in $(seq 1 30); do
  systemctl --user is-system-running >/dev/null 2>&1 && break
  sleep 0.5
done

echo "==> building pinner"
export PATH="$PATH:/root/.local/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin"
if [ -z "${PINNER_BIN:-}" ]; then
  PINNER_BIN="$(mktemp -d)/pinner"
fi
if [ ! -x "${PINNER_BIN}" ]; then
  CGO_ENABLED=1 go build -o "${PINNER_BIN}" ./cmd/pinner
fi
echo "    pinner binary: ${PINNER_BIN}"
export PINNER_BIN

echo "==> running systemd user-service integration test"
go test -tags integration -count=1 -v \
  -run 'TestSystemdUserServiceInstallServesOAuth' \
  ./internal/service/

echo "PASS: managed pinner MCP service installed, started, and served under real systemd"
