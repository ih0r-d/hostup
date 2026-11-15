#!/usr/bin/env bash
# Shared host-side prerequisites; source this file from other VM scripts.
# Sourcing only defines functions; entry points check tools before doing work.

# require_command COMMAND INSTALL_HINT
# Stop before contacting the VM if a required host tool is unavailable.
require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "FAIL: $1 is required. $2" >&2
    return 1
  fi
}

require_multipass() {
  require_command multipass 'Install Multipass before running VM scripts.'
}
