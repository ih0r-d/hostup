#!/usr/bin/env bash
# Exercise Hostup against a disposable systemd service in a running Ubuntu VM.
# Usage: integration.sh VM BINARY
# BINARY is an already deployed guest executable. Requires Multipass on the host
# and passwordless sudo in the guest. Only the uniquely named test unit changes;
# exit traps remove it and its temporary files, including after failed assertions.

# shellcheck source=.dev/vm/common.sh
source "${BASH_SOURCE[0]%/*}/common.sh"

run_integration() {
  local vm=$1 binary=$2
  # Everything in this quoted heredoc runs as root inside the VM. Pass host
  # values as positional arguments so only the guest expands its variables.
  multipass exec "$vm" -- sudo -n bash -seuo pipefail -- "$binary" <<'SCRIPT'
# Report diagnostics before deleting evidence. Continue through cleanup errors,
# preserve the original failure, and return nonzero if cleanup itself fails.
# The ownership flag prevents cleanup from touching a unit we did not create.
finish() {
  local result=$?
  trap - EXIT
  set +e
  if [ "$result" -ne 0 ]; then
    echo "FAIL integration: $step" >&2
    cat "$work/output" >&2
    systemctl status --no-pager "$unit" >&2
    journalctl --no-pager -u "$unit" -n 30 >&2
  fi
  if "$owned"; then
    local cleanup=0
    systemctl stop "$unit" || cleanup=1
    systemctl disable "$unit" || cleanup=1
    # reset-failed returns nonzero for a unit that is already inactive.
    if [ "$(systemctl show -p ActiveState --value "$unit")" = failed ]; then
      systemctl reset-failed "$unit" || cleanup=1
    fi
    rm -f -- "$unit_file" || cleanup=1
    systemctl daemon-reload || cleanup=1
    if [ "$cleanup" -ne 0 ]; then
      echo "FAIL integration: cleanup of $unit" >&2
      result=1
    fi
  fi
  rm -rf -- "$work" || result=1
  if [ "$result" -eq 0 ]; then echo 'PASS integration: cleanup'; fi
  exit "$result"
}

# Keep successful commands quiet and retain the latest output for diagnostics.
run() { "$@" >"$work/output" 2>&1; }

# Assert real systemd properties independently of Hostup's success message.
state() {
  local actual
  actual=$(systemctl show -p "$1" --value "$unit")
  if [ "$actual" != "$2" ]; then
    echo "Expected $1=$2; got $actual" >&2
    return 1
  fi
}

# Inactive and failed units must produce usable text and JSON status reports.
status() {
  step="status ($1)"
  run "$binary" service status "$unit"
  grep -Eq "Active: $1([ (]|$)" "$work/output"
  run "$binary" service status "$unit" --json
  grep -Fq "\"active\":\"$1\"" "$work/output"
  state ActiveState "$1"
  echo "PASS integration: $step"
}

create_test_unit() {
  # Refuse collisions, including vendor units and dangling symlinks.
  test ! -e "$unit_file"
  test ! -L "$unit_file"
  state LoadState not-found
  # Claim the name before writing so cleanup never removes an existing unit.
  (set -C; : >"$unit_file")
  owned=true
  # A long-running process gives restart a new PID and writes a journal marker.
  # Restart=no keeps an intentional failure observable until cleanup.
  cat >"$unit_file" <<UNIT
[Unit]
Description=Hostup disposable integration test
[Service]
Type=simple
ExecStart=/bin/sh -c 'echo $unit-started; exec /usr/bin/sleep infinity'
Restart=no
StandardOutput=journal
[Install]
WantedBy=multi-user.target
UNIT
  systemctl daemon-reload
  state LoadState loaded
  state ActiveState inactive
  state UnitFileState disabled
}

test_start() {
  step=start
  run "$binary" service start "$unit" --yes
  state ActiveState active
  state SubState running
  echo 'PASS integration: start'
  status active
}

test_list() {
  step=list
  run "$binary" service list
  awk -v unit="$unit" '$1 == unit && $2 == "loaded" && $3 == "active" && $4 == "running" { found=1 } END { exit !found }' "$work/output"
  state ActiveState active
  echo 'PASS integration: list'
}

test_restart() {
  local pid next_pid
  # An active state alone cannot prove restart happened; require a different PID.
  step=restart
  pid=$(systemctl show -p MainPID --value "$unit")
  test "$pid" -gt 0
  run "$binary" service restart "$unit" --yes
  state ActiveState active
  state SubState running
  next_pid=$(systemctl show -p MainPID --value "$unit")
  test "$next_pid" -gt 0
  test "$next_pid" != "$pid"
  echo 'PASS integration: restart'
}

# Enable/disable affect boot configuration while the service keeps running.
test_enablement() {
  local action=$1 expected=$2
  step=$action
  run "$binary" service "$action" "$unit" --yes
  state UnitFileState "$expected"
  state ActiveState active
  echo "PASS integration: $step"
}

test_logs() {
  local pid attempt
  pid=$(systemctl show -p MainPID --value "$unit")
  step=logs
  # The shell emits the marker before exec'ing sleep; wait for that exec.
  for ((attempt=0; attempt<50; attempt++)); do
    [ "$(cat "/proc/$pid/comm")" = sleep ] && break
    sleep 0.1
  done
  test "$(cat "/proc/$pid/comm")" = sleep
  journalctl --sync
  run "$binary" service logs "$unit" --lines 20
  grep -Fq "$unit-started" "$work/output"
  state ActiveState active
  echo 'PASS integration: logs'
}

test_stop() {
  step=stop
  run "$binary" service stop "$unit" --yes
  state ActiveState inactive
  state SubState dead
  echo 'PASS integration: stop'
  status inactive
}

test_failed_status() {
  local attempt
  # Kill only our test process to produce a genuine failed state. Bound the wait
  # because systemd records the exit asynchronously; never rely on a fixed sleep.
  step='failed status setup'
  run "$binary" service start "$unit" --yes
  state ActiveState active
  systemctl kill --signal=KILL --kill-whom=main "$unit"
  for ((attempt=0; attempt<50; attempt++)); do
    [ "$(systemctl show -p ActiveState --value "$unit")" = failed ] && break
    sleep 0.1
  done
  state ActiveState failed
  state SubState failed
  status failed
}

main() {
  # Shared guest context stays available to the EXIT trap after main returns.
  binary=$1
  work=$(mktemp -d /run/hostup-integration.XXXXXX)
  unit="${work##*/}.service"
  unit_file="/etc/systemd/system/$unit"
  owned=false
  step=setup
  export LC_ALL=C SYSTEMD_COLORS=0 SYSTEMD_PAGER=cat
  trap finish EXIT
  trap 'exit 130' INT
  trap 'exit 143' HUP TERM

  create_test_unit
  status inactive
  test_start
  test_list
  test_restart
  test_enablement enable enabled
  test_enablement disable disabled
  test_logs
  test_stop
  test_failed_status
}

main "$@"
SCRIPT
}

main() {
  require_multipass
  local vm=${1:?Usage: integration.sh VM BINARY}
  local binary=${2:?Usage: integration.sh VM BINARY}
  run_integration "$vm" "$binary"
}

# Sourcing exposes functions without contacting the VM.
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  set -euo pipefail
  main "$@"
fi
