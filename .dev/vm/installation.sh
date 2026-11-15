#!/usr/bin/env bash
# Manage a locally built Hostup installation in an existing, running VM.
# Usage: installation.sh install|verify|uninstall VM BINARY [VERSION]
# BINARY is the deployed path inside Ubuntu; VERSION is required for verify.
# The host needs Multipass. Guest file operations use passwordless sudo;
# CLI verification runs as the normal Multipass user with its existing PATH.
# Installation ownership for /usr/local/bin/hostup is recorded in /var/lib/hostup-dev.

# shellcheck source=.dev/vm/common.sh
source "${BASH_SOURCE[0]%/*}/common.sh"

manage_installation() {
  local vm=$1 action=$2 binary=$3
  local installer=${BASH_SOURCE[0]%/*}/../install.sh
  # Stream the shared installer; keep all ownership and completion logic in one
  # place, without transferring auxiliary scripts or changing guest shell config.
  multipass exec "$vm" -- sudo -n bash -seuo pipefail -- "$action" "$binary" <"$installer"
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

# Ubuntu's normal shell startup must discover the installed profile itself.
# Never source Hostup's file or change the user's startup files for this check.
verify_completion() {
  local login shell
  login=$(getent passwd "$(id -un)")
  shell=${login##*:}
  step="fresh ${shell##*/} completion"
  case "${shell##*/}" in
    bash)
      bash -ic 'declare -F _completion_loader >/dev/null || { echo "Install bash-completion and open a new Bash session; completion support is not enabled." >&2; exit 1; }; _completion_loader hostup; complete -p hostup' >"$output" 2>&1
      ;;
    zsh)
      zsh -ic '(( $+functions[compdef] )) && [[ ${_comps[hostup]} == _hostup ]]' >"$output" 2>&1
      ;;
    fish)
      fish -ic "complete -C 'hostup ser' | string match -q 'service*'" >"$output" 2>&1
      ;;
    *) echo "Unsupported login shell: $shell" >&2; return 1 ;;
  esac
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
  verify_completion
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
