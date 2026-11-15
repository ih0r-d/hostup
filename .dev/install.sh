#!/usr/bin/env bash
# Local Linux installation lifecycle, shared by native and Multipass workflows.
# Usage: bash .dev/install.sh install|verify|uninstall [LOCAL_BINARY]
# Detect the invoking shell and generate one native Cobra completion profile.
# DESTDIR stages an isolated filesystem tree (also usable without root on macOS).
# No shell startup files or packages are changed. No automatic sudo is invoked.
set -euo pipefail

die() { echo "FAIL installation: $*" >&2; exit 1; }
exists() { [ -e "$1" ] || [ -L "$1" ]; }

metadata() {
  stat -c '%u:%a' "$1" 2>/dev/null || stat -f '%u:%Lp' "$1"
}

checksum() {
  local digest
  if command -v sha256sum >/dev/null 2>&1; then
    digest=$(sha256sum "$1")
  else
    digest=$(shasum -a 256 "$1")
  fi
  printf '%s\n' "${digest%% *}"
}

# SHELL is the login shell and may differ from the active terminal. Look through
# sudo/launcher parents first, then fall back to the invoking user's login shell.
detect_shell() {
  local pid=$PPID info command parent name record='' candidate='' user attempt
  if command -v ps >/dev/null 2>&1; then
    for ((attempt=0; attempt<8; attempt++)); do
      info=$(ps -p "$pid" -o comm= -o ppid= 2>/dev/null) || break
      read -r command parent <<<"$info"
      name=${command##*/}
      case "${name#-}" in bash|zsh|fish) printf '%s\n' "${name#-}"; return ;; esac
      case "$parent" in ''|*[!0-9]*|0|1) break ;; esac
      pid=$parent
    done
  fi
  user=${SUDO_USER:-$(id -un)}
  if command -v getent >/dev/null 2>&1; then
    record=$(getent passwd "$user" 2>/dev/null) || record=
  fi
  if [ -n "$record" ]; then candidate=${record##*:}; else candidate=${SHELL:-}; fi
  name=${candidate##*/}
  case "$name" in bash|zsh|fish) printf '%s\n' "$name" ;;
    *) die "cannot identify a supported active shell; run installation from Bash, Zsh, or Fish" ;;
  esac
}

lock_installation() {
  if [ -z "$root" ]; then
    command -v flock >/dev/null 2>&1 || die 'flock is required (Ubuntu: install util-linux)'
    exec 9>/run/hostup-dev-install.lock
    flock -n 9 || die 'another Hostup installation is running'
  else
    # Staging must not touch the host's /run; a temporary directory lock also
    # works on macOS, where flock is not part of the base system.
    mkdir "$root/.hostup-install-lock" || die 'another installation is staging into DESTDIR'
    staging_lock=true
  fi
}

validate_directory() {
  if exists "$directory"; then
    test -d "$directory"
    test ! -L "$directory"
    test "$(metadata "$directory")" = "$owner:700"
  fi
}

validate_files() {
  local i receipt digest
  validate_directory
  for i in 0 1 2 3; do
    receipt="$directory/${receipts[i]}"
    if exists "$receipt"; then
      test -f "$receipt"
      test ! -L "$receipt"
      test "$(metadata "$receipt")" = "$owner:600"
      digest=$(cat "$receipt")
      [[ "$digest" =~ ^[0-9a-f]{64}$ ]]
      if exists "${paths[i]}"; then
        test -f "${paths[i]}"
        test ! -L "${paths[i]}"
        test "$(metadata "${paths[i]}")" = "$owner:${modes[i]}"
        test "$(checksum "${paths[i]}")" = "$digest"
      fi
    elif [ "$i" -eq 0 ] || { [ "$action" != uninstall ] && [ "$i" -eq "$selected" ]; }; then
      # Identical content without a receipt is still user-managed, not ours.
      if exists "${paths[i]}"; then die "refusing unmanaged file ${paths[i]}"; fi
    fi
  done
  # Older bash-completion versions try the unsuffixed name before .bash. Do not
  # shadow a user's profile with another filename in the same autoload directory.
  if [ "$action" = install ] && [ "$selected" -eq 1 ] && exists "${paths[1]%.bash}"; then
    die "preserving existing Bash profile ${paths[1]%.bash}"
  fi
}

prepare_transaction() {
  local i receipt
  work=$(mktemp -d)
  for i in 0 1 2 3; do
    receipt="$directory/${receipts[i]}"
    if [ -f "$receipt" ] || [ "$i" -eq 0 ] || [ "$i" -eq "$selected" ]; then
      touched[i]=true
      if exists "${paths[i]}"; then cp -p "${paths[i]}" "$work/old-$i"; fi
      if exists "$receipt"; then cp -p "$receipt" "$work/receipt-$i"; fi
    fi
  done
}

stage_installation() {
  local i
  test -f "$source"
  test -x "$source"
  prepare_transaction
  install -m 755 "$source" "$work/new-0"
  "$work/new-0" completion "$shell" >"$work/new-$selected"
  test -s "$work/new-$selected"
  install -d -m 700 "$directory"
  for i in 0 "$selected"; do
    mkdir -p "${paths[i]%/*}"
    stages[i]=$(mktemp "${paths[i]%/*}/.hostup-install.XXXXXX")
    install -m "${modes[i]}" "$work/new-$i" "${stages[i]}"
    receipt_stages[i]=$(mktemp "$directory/.receipt.XXXXXX")
    checksum "${stages[i]}" >"${receipt_stages[i]}"
    chmod 600 "${receipt_stages[i]}"
  done
}

install_files() {
  local i
  stage='generate and stage binary/completion'
  stage_installation
  stage='recheck ownership before publishing'
  validate_files
  stage='publish installation'
  committing=true
  for i in 0 "$selected"; do
    mv -f "${stages[i]}" "${paths[i]}"
    mv -f "${receipt_stages[i]}" "$directory/${receipts[i]}"
  done
  # Switching shells replaces only previous, checksum-verified Hostup profiles.
  # Other user-managed completion files are left alone.
  for i in 1 2 3; do
    if [ "$i" -ne "$selected" ] && [ -f "$directory/${receipts[i]}" ]; then
      rm -f "${paths[i]}" "$directory/${receipts[i]}"
    fi
  done
  committing=false
  echo "PASS installation: hostup and $shell completion installed"
  bash_support
}

bash_support() {
  if [ "$selected" -eq 1 ] &&
    [ ! -s "$root/usr/share/bash-completion/bash_completion" ] && [ ! -s "$root/etc/bash_completion" ]; then
    echo 'WARN completion: Bash completion support is missing. On Ubuntu, run: sudo apt install bash-completion; then open a new Bash session.' >&2
  fi
}

verify_files() {
  local i
  for i in 0 "$selected"; do
    test -f "${paths[i]}"
    test -f "$directory/${receipts[i]}"
  done
  test "$(checksum "$source")" = "$(cat "$directory/sha256")"
  echo "PASS installation: binary and $shell completion ownership, checksums and modes"
  bash_support
}

uninstall_files() {
  local i
  prepare_transaction
  committing=true
  for i in 0 1 2 3; do
    if [ -f "$directory/${receipts[i]}" ]; then
      rm -f "${paths[i]}" "$directory/${receipts[i]}"
    fi
  done
  committing=false
  if [ -d "$directory" ]; then
    rmdir "$directory" 2>/dev/null || echo "WARN installation: preserved nonempty $directory" >&2
  fi
  echo 'PASS installation: managed binary and completion files absent'
}

# Roll back catchable publication failures, including receipts, before removing
# staging files. Never include unowned, unselected profiles in this transaction.
finish() {
  local result=$? i
  trap - EXIT
  set +e
  if [ "$result" -ne 0 ]; then
    echo "FAIL installation: $action ($stage)" >&2
    if "$committing"; then
      for i in 0 1 2 3; do
        if "${touched[i]}"; then
          if [ -f "$work/old-$i" ]; then cp -p "$work/old-$i" "${paths[i]}"; else rm -f "${paths[i]}"; fi
          if [ -f "$work/receipt-$i" ]; then cp -p "$work/receipt-$i" "$directory/${receipts[i]}"; else rm -f "$directory/${receipts[i]}"; fi
        fi
      done
    fi
  fi
  for i in 0 1 2 3; do
    if [ -n "${stages[i]}" ]; then rm -f "${stages[i]}" || result=1; fi
    if [ -n "${receipt_stages[i]}" ]; then rm -f "${receipt_stages[i]}" || result=1; fi
  done
  if [ -n "$work" ]; then rm -rf "$work" || result=1; fi
  if "$staging_lock"; then rmdir "$root/.hostup-install-lock" || result=1; fi
  if [ "$result" -ne 0 ] && [ -d "$directory" ]; then rmdir "$directory" 2>/dev/null; fi
  exit "$result"
}

main() {
  action=${1:?Usage: install.sh install|verify|uninstall [LOCAL_BINARY]}
  source=${2:-}
  root=${DESTDIR:-}
  root=${root%/}
  case "$action" in install|verify|uninstall) ;; *) die "unknown action $action" ;; esac
  if [ -z "$root" ]; then
    test "$(uname -s)" = Linux || die 'native installation requires Linux'
    test "$EUID" -eq 0 || die 'installation requires root privileges; invoke it explicitly'
  else
    [[ "$root" = /* ]] || die 'DESTDIR must be an absolute staging directory'
    test -d "$root" || die 'DESTDIR must already exist'
  fi
  owner=$EUID
  directory="$root/var/lib/hostup-dev"
  paths=("$root/usr/local/bin/hostup" "$root/usr/share/bash-completion/completions/hostup.bash"
    "$root/usr/local/share/zsh/site-functions/_hostup" "$root/usr/share/fish/vendor_completions.d/hostup.fish")
  receipts=(sha256 completion-bash.sha256 completion-zsh.sha256 completion-fish.sha256)
  modes=(755 644 644 644)
  stages=('' '' '' '')
  receipt_stages=('' '' '' '')
  touched=(false false false false)
  shell='' selected=-1 work='' committing=false staging_lock=false stage=preflight
  trap finish EXIT
  trap 'exit 130' INT
  trap 'exit 143' HUP TERM
  if [ "$action" != uninstall ]; then
    shell=$(detect_shell)
    case "$shell" in bash) selected=1 ;; zsh) selected=2 ;; fish) selected=3 ;; esac
  fi
  lock_installation
  validate_files
  case "$action" in install) install_files ;; verify) verify_files ;; uninstall) uninstall_files ;; esac
}

main "$@"
