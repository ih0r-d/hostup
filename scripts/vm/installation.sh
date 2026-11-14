#!/usr/bin/env bash
# Manage a locally built Hostup installation in an existing, running VM.
# Usage: installation.sh install|verify|uninstall VM BINARY [VERSION]
# BINARY is the deployed path inside Ubuntu; VERSION is required for verify.
# The host needs Multipass. Guest file operations use passwordless sudo;
# CLI verification runs as the normal Multipass user with its existing PATH.
# Installation ownership for /usr/local/bin/hostup is recorded in /var/lib/hostup-dev.

# shellcheck source=scripts/vm/common.sh
source "${BASH_SOURCE[0]%/*}/common.sh"

manage_installation() {
  local vm=$1 action=$2 binary=$3
  # Guest operations run as root; the quoted heredoc keeps expansion in Ubuntu.
  multipass exec "$vm" -- sudo -n bash -seuo pipefail -- "$action" "$binary" <<'SCRIPT'
# Preserve the failing exit code and remove staging files on every normal exit
# or catchable interruption. Diagnostic failures must not skip cleanup.
finish() {
  local result=$?
  trap - EXIT
  set +e
  if [ "$result" -ne 0 ]; then
    echo "FAIL installation: $action ($step)" >&2
    ls -ld "$binary" "$directory" "$receipt" >&2 || true
  fi
  if [ -n "$stage" ]; then rm -f -- "$stage" || result=1; fi
  if [ -n "$receipt_stage" ]; then rm -f -- "$receipt_stage" || result=1; fi
  exit "$result"
}

lock_installation() {
  # Serialize install, verification and uninstall without another daemon.
  exec 9>/run/hostup-dev-install.lock
  flock -n 9
}

validate_ownership() {
  local digest
  # A protected receipt records the last installed SHA-256 digest. Validate both
  # metadata and contents before updating or deleting an existing installation.
  # Reject symlinks explicitly, including dangling ones that test -e cannot see.
  step='ownership and checksum validation; refusing unmanaged or modified files'
  if [ -e "$directory" ] || [ -L "$directory" ]; then
    test -d "$directory"
    test ! -L "$directory"
    test "$(stat -c '%u:%a' "$directory")" = 0:700
    test -f "$receipt"
    test ! -L "$receipt"
    test "$(stat -c '%u:%a' "$receipt")" = 0:600
    grep -Eq '^[0-9a-f]{64}$' "$receipt"
    if [ -e "$binary" ] || [ -L "$binary" ]; then
      test -f "$binary"
      test ! -L "$binary"
      test "$(stat -c '%u:%a' "$binary")" = 0:755
      digest=$(sha256sum "$binary")
      test "${digest%% *}" = "$(cat "$receipt")"
    fi
  else
    # Even an identical binary needs a receipt proving workflow ownership.
    test ! -e "$binary"
    test ! -L "$binary"
  fi
}

install_binary() {
  local digest
  step='stage local build'
  test -f "$source"
  test -x "$source"
  # Stage beside the destination so each rename stays on the same filesystem.
  # Each file is replaced atomically; binary and receipt are separate renames.
  stage=$(mktemp /usr/local/bin/.hostup-dev.XXXXXX)
  install -m 755 "$source" "$stage"
  digest=$(sha256sum "$stage")
  if [ ! -d "$directory" ]; then install -d -m 700 "$directory"; fi
  receipt_stage=$(mktemp "$directory/.sha256.XXXXXX")
  printf '%s\n' "${digest%% *}" >"$receipt_stage"
  chmod 600 "$receipt_stage"
  step='install binary and receipt'
  mv -f -- "$stage" "$binary"
  mv -f -- "$receipt_stage" "$receipt"
  echo 'PASS installation: /usr/local/bin/hostup installed'
}

verify_binary() {
  local digest
  # The shared checks establish ownership; also require the current local build.
  step='compare installed binary with deployed build'
  test -f "$receipt"
  test -x "$binary"
  digest=$(sha256sum "$source")
  test "${digest%% *}" = "$(cat "$receipt")"
  echo 'PASS installation: ownership, checksum and mode 0755'
}

uninstall_binary() {
  # A missing managed binary is safe to clean up; unrelated files were rejected
  # above. rmdir refuses to remove a receipt directory containing other files.
  step='remove managed binary and receipt'
  if [ -f "$receipt" ]; then
    rm -f -- "$binary"
    rm -- "$receipt"
    rmdir -- "$directory"
  fi
  echo 'PASS installation: development binary absent'
}

main() {
  # Shared guest context is used by the action functions and EXIT trap.
  action=$1
  source=$2
  binary=/usr/local/bin/hostup
  directory=/var/lib/hostup-dev
  receipt=$directory/sha256
  stage=
  receipt_stage=
  step=lock
  trap finish EXIT
  trap 'exit 130' INT
  trap 'exit 143' HUP TERM

  lock_installation
  case "$action" in install|verify|uninstall) ;; *) return 1 ;; esac
  validate_ownership
  case "$action" in
    install) install_binary ;;
    verify) verify_binary ;;
    uninstall) uninstall_binary ;;
  esac
}

main "$@"
SCRIPT
}

verify_cli() {
  local vm=$1 expected=$2
  # Root can have a different PATH: check the normal VM user without editing it.
  multipass exec "$vm" -- bash -seuo pipefail -- "$expected" <<'SCRIPT'
# Preserve diagnostics and remove the captured CLI output on every exit.
finish() {
  local result=$?
  trap - EXIT
  set +e
  if [ "$result" -ne 0 ]; then
    echo "FAIL installation: $step (PATH=$PATH)" >&2
    cat "$output" >&2
    ls -l /usr/local/bin/hostup >&2
  fi
  rm -f -- "$output" || result=1
  exit "$result"
}

verify_path() {
  test "$(command -v hostup)" = /usr/local/bin/hostup
  test -x /usr/local/bin/hostup
  echo 'PASS installation: executable on PATH'
}

verify_version() {
  local flag
  for flag in version --version; do
    step=$flag
    hostup "$flag" >"$output" 2>&1
    test "$(cat "$output")" = "hostup $expected"
    echo "PASS installation: $step"
  done
}

verify_commands() {
  local command
  for command in --help info doctor; do
    step=$command
    hostup "$command" >"$output" 2>&1
    test -s "$output"
    echo "PASS installation: $step"
  done
  step='service list'
  hostup service list >"$output" 2>&1
  test -s "$output"
  echo "PASS installation: $step"
}

main() {
  # Keep diagnostics available to the EXIT trap after the checks return.
  expected=$1
  step=PATH
  output=$(mktemp)
  trap finish EXIT
  trap 'exit 130' INT
  trap 'exit 143' HUP TERM
  verify_path
  verify_version
  verify_commands
}

main "$@"
SCRIPT
}

main() {
  require_multipass
  local action=${1:?Usage: installation.sh install|verify|uninstall VM BINARY [VERSION]}
  local vm=${2:?VM name is required}
  local binary=${3:?Deployed binary path is required}
  local expected
  case "$action" in
    install|uninstall) ;;
    verify) expected=${4:?Expected version is required for verification} ;;
    *) echo "FAIL: unknown installation action: $action" >&2; return 1 ;;
  esac
  manage_installation "$vm" "$action" "$binary"
  if [ "$action" = verify ]; then
    verify_cli "$vm" "$expected"
  fi
}

# Sourcing exposes functions without contacting the VM.
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  set -euo pipefail
  main "$@"
fi
