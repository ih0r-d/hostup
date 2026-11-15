#!/usr/bin/env bash
# Create/start or stop the development VM from the host. Never delete it.
# Usage: lifecycle.sh up VM UBUNTU CPUS MEMORY DISK
#        lifecycle.sh down VM
# Requires Multipass and jq on the host. Resource arguments apply only at launch;
# existing VMs retain their configuration and disk contents.
# shellcheck source=.dev/vm/common.sh
source "${BASH_SOURCE[0]%/*}/common.sh"

# Select this VM by name; absent entries become Missing rather than an error.
vm_state() {
  local vm=$1 listing
  listing=$(multipass list --format json) || return
  printf '%s\n' "$listing" | jq -er --arg name "$vm" \
    '[.list[] | select(.name == $name) | .state][0] // "Missing"'
}

start_vm() {
  local vm=$1 ubuntu=$2 cpus=$3 memory=$4 disk=$5 state
  state=$(vm_state "$vm")
  case "$state" in
    Missing)
      multipass launch "$ubuntu" --name "$vm" \
        --cpus "$cpus" --memory "$memory" --disk "$disk"
      ;;
    Stopped|Suspended) multipass start "$vm" ;;
    Running) echo "$vm is already running." ;;
    *)
      # Transitional or deleted states need inspection, not automatic recovery.
      echo "Cannot start $vm in state $state; inspect it with multipass info." >&2
      return 1
      ;;
  esac
}

# Stopping is repeatable: an absent or already stopped VM needs no action.
stop_vm() {
  local vm=$1 state
  state=$(vm_state "$vm")
  case "$state" in
    Running|Suspended) multipass stop "$vm" ;;
    Missing|Stopped|Deleted) echo "$vm is already stopped or absent ($state)." ;;
    *)
      echo "Cannot stop $vm in state $state; inspect it with multipass info." >&2
      return 1
      ;;
  esac
}

main() {
  require_multipass
  require_command jq 'Install jq to parse Multipass JSON output.'
  local action=${1:?Usage: lifecycle.sh up|down VM [UBUNTU CPUS MEMORY DISK]}
  local vm=${2:?VM name is required}
  case "$action" in
    up)
      start_vm "$vm" "${3:?Ubuntu version is required}" "${4:?CPU count is required}" \
        "${5:?Memory size is required}" "${6:?Disk size is required}"
      ;;
    down) stop_vm "$vm" ;;
    *) echo "FAIL: unknown VM lifecycle action: $action" >&2; return 1 ;;
  esac
}

# Sourcing exposes functions without starting or stopping a VM.
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  set -euo pipefail
  main "$@"
fi
