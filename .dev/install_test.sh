#!/usr/bin/env bash
# Exercise the local installer in disposable DESTDIR trees, without root or VMs.
# Uses the real CLI for Cobra generation; fake process lookups select each shell.
# Usage: bash .dev/install_test.sh
# Fixture scripts intentionally keep variables literal until their subprocess runs.
# shellcheck disable=SC2016
set -euo pipefail

fail() { echo "FAIL installer test: $*" >&2; exit 1; }
put() { mkdir -p "${1%/*}"; printf '%s\n' "$2" >"$1"; }
absent() { [ ! -e "$1" ] && [ ! -L "$1" ] || fail "unexpected file: $1"; }
contains() { [[ "$output" == *"$1"* ]] || fail "missing output: $1 ($output)"; }

fixture() {
  root=$(mktemp -d "$work/root.XXXXXX")
  tools=$(mktemp -d "$work/tools.XXXXXX")
  source="$tools/hostup source"
  shell=bash
  login=bash
  env_shell=fish
  ps_fail=0
  getent_fail=0
  generation_fail=''
  mv_counter=''
  put "$source" '#!/usr/bin/env bash
if [ "${HOSTUP_TEST_GENERATION_FAIL:-}" = "$2" ]; then exit 17; fi
exec "$HOSTUP_TEST_BINARY" "$@"'
  put "$tools/ps" '#!/bin/sh
[ "$HOSTUP_TEST_PS_FAIL" != 1 ] || exit 1
printf "%s 1\n" "$HOSTUP_TEST_ACTIVE_SHELL"'
  put "$tools/getent" '#!/bin/sh
[ "$HOSTUP_TEST_GETENT_FAIL" != 1 ] || exit 1
printf "fixture:x:1000:1000:fixture:/nonexistent:/bin/%s\n" "$HOSTUP_TEST_LOGIN_SHELL"'
  chmod 755 "$source" "$tools/ps" "$tools/getent"
}

profile() {
  case "$1" in
    bash) printf '%s\n' "$root/usr/share/bash-completion/completions/hostup.bash" ;;
    zsh) printf '%s\n' "$root/usr/local/share/zsh/site-functions/_hostup" ;;
    fish) printf '%s\n' "$root/usr/share/fish/vendor_completions.d/hostup.fish" ;;
  esac
}

run_installer() {
  local action=$1 expected=$2 result=0 path
  output=$(DESTDIR="$root" PATH="$tools:$PATH" SHELL="/bin/$env_shell" \
    HOSTUP_TEST_BINARY="$binary" HOSTUP_TEST_ACTIVE_SHELL="$shell" \
    HOSTUP_TEST_LOGIN_SHELL="$login" HOSTUP_TEST_PS_FAIL="$ps_fail" \
    HOSTUP_TEST_GETENT_FAIL="$getent_fail" HOSTUP_TEST_GENERATION_FAIL="$generation_fail" \
    HOSTUP_TEST_MV_COUNTER="$mv_counter" HOSTUP_TEST_REAL_MV="$real_mv" \
    bash "$installer" "$action" "$source" 2>&1) || result=$?
  if { [ "$expected" = pass ] && [ "$result" -ne 0 ]; } ||
    { [ "$expected" = fail ] && [ "$result" -eq 0 ]; }; then
    fail "$action expected $expected, exit $result: $output"
  fi
  absent "$root/.hostup-install-lock"
  # Glob in Bash rather than depending on additional filesystem test tools.
  while IFS= read -r path; do
    case "${path##*/}" in .hostup-install.*|.receipt.*) fail "leaked staging file $path" ;; esac
  done < <(find "$root" -type f)
}

test_lifecycle() {
  local selected other first path
  for selected in bash zsh fish; do
    fixture
    shell=$selected
    put "$root/etc/bash.bashrc" 'global configuration'
    put "$root/home/fixture/.bashrc" 'user configuration'
    put "$root/usr/share/bash-completion/bash_completion" '# framework fixture'
    run_installer install pass
    run_installer verify pass
    path=$(profile "$shell")
    first=$(cat "$path")
    [[ "$first" == *'__complete'* ]] || fail 'not a Cobra profile'
    [ -x "$root/usr/local/bin/hostup" ] || fail 'binary not executable'
    for other in bash zsh fish; do
      if [ "$other" != "$shell" ]; then absent "$(profile "$other")"; fi
    done
    run_installer install pass
    [ "$(cat "$path")" = "$first" ] || fail 'reinstallation changed profile'
    run_installer uninstall pass
    run_installer uninstall pass
    absent "$path"
    absent "$root/usr/local/bin/hostup"
    absent "$root/var/lib/hostup-dev"
    [ "$(cat "$root/etc/bash.bashrc")" = 'global configuration' ] || fail 'global rc changed'
    [ "$(cat "$root/home/fixture/.bashrc")" = 'user configuration' ] || fail 'user rc changed'
    echo "PASS installer: $shell lifecycle, single profile, permissions and rc preservation"
  done
}

test_detection() {
  fixture; shell=zsh
  run_installer install pass
  [ -f "$(profile zsh)" ] || fail 'active shell not selected'
  fixture; ps_fail=1; login=fish; env_shell=bash
  run_installer install pass
  [ -f "$(profile fish)" ] || fail 'login fallback failed'
  fixture; ps_fail=1; getent_fail=1; env_shell=zsh
  run_installer install pass
  [ -f "$(profile zsh)" ] || fail 'environment fallback failed'
  fixture; shell=unknown; login='sh'; env_shell='sh'
  run_installer install fail
  absent "$root/usr/local/bin/hostup"
  fixture
  run_installer install pass
  contains 'apt install bash-completion'
  contains 'new Bash session'
  echo 'PASS installer: active shell, fallbacks and missing Bash support'
}

test_ownership() {
  local selected path target
  for selected in binary bash zsh fish bash-unsuffixed; do
    fixture
    shell=$selected
    case "$selected" in
      binary) shell=bash; path="$root/usr/local/bin/hostup" ;;
      bash-unsuffixed) shell=bash; path="$(profile bash)"; path=${path%.bash} ;;
      *) path=$(profile "$shell") ;;
    esac
    put "$path" 'user-managed file'
    run_installer install fail
    [ "$(cat "$path")" = 'user-managed file' ] || fail 'unowned file overwritten'
    if [ "$selected" != binary ]; then
      absent "$root/usr/local/bin/hostup"
      run_installer uninstall pass
      [ "$(cat "$path")" = 'user-managed file' ] || fail 'unowned file removed'
    fi
  done
  fixture
  run_installer install pass
  put "$(profile bash)" 'modified profile'
  run_installer install fail
  run_installer uninstall fail
  [ "$(cat "$(profile bash)")" = 'modified profile' ] || fail 'modified profile changed'
  for target in "$work/external" "$work/missing"; do
    fixture
    put "$work/external" 'external data'
    path=$(profile bash)
    mkdir -p "${path%/*}"
    ln -s "$target" "$path"
    run_installer install fail
    [ "$(readlink "$path")" = "$target" ] || fail 'symlink replaced'
    [ "$(cat "$work/external")" = 'external data' ] || fail 'symlink target changed'
  done
  echo 'PASS installer: unmanaged files, modified profiles and symlinks preserved'
}

test_upgrade_and_failure() {
  local before receipt
  fixture
  run_installer install pass
  rm "$(profile bash)" "$root/var/lib/hostup-dev/completion-bash.sha256"
  run_installer install pass # Upgrade the previous binary-only receipt format.
  put "$(profile fish)" 'custom fish completion'
  shell=zsh
  run_installer install pass
  absent "$(profile bash)"
  run_installer uninstall pass
  [ "$(cat "$(profile fish)")" = 'custom fish completion' ] || fail 'custom profile removed'
  fixture; generation_fail=bash
  run_installer install fail
  absent "$root/usr/local/bin/hostup"
  absent "$(profile bash)"
  generation_fail=''
  run_installer install pass
  before=$(cat "$root/usr/local/bin/hostup")
  receipt=$(cat "$root/var/lib/hostup-dev/sha256")
  printf '\n# changed build\n' >>"$source"
  mv_counter="$tools/counter"
  put "$tools/mv" '#!/bin/sh
n=0
[ ! -f "$HOSTUP_TEST_MV_COUNTER" ] || n=$(cat "$HOSTUP_TEST_MV_COUNTER")
n=$((n+1)); echo "$n" >"$HOSTUP_TEST_MV_COUNTER"
[ "$n" != 3 ] || exit 42
exec "$HOSTUP_TEST_REAL_MV" "$@"'
  chmod 755 "$tools/mv"
  run_installer install fail
  [ "$(cat "$root/usr/local/bin/hostup")" = "$before" ] || fail 'binary rollback failed'
  [ "$(cat "$root/var/lib/hostup-dev/sha256")" = "$receipt" ] || fail 'receipt rollback failed'
  rm "$tools/mv"
  run_installer install pass
  run_installer verify pass
  rm "$root/usr/local/bin/hostup" "$(profile bash)"
  run_installer uninstall pass
  run_installer uninstall pass
  echo 'PASS installer: legacy upgrade, shell switch, rollback, retry and missing files'
}

test_zsh_autoload() {
  if ! command -v zsh >/dev/null 2>&1; then echo 'SKIP installer: Zsh runtime unavailable'; return; fi
  fixture; shell=zsh
  run_installer install pass
  # Simulate the standard system fpath in the isolated tree. compinit discovers
  # #compdef itself; -D prevents persistent completion cache files.
  HOME="$root/home" HOSTUP_TEST_FPATH="$(dirname "$(profile zsh)")" zsh -fc \
    'fpath=("$HOSTUP_TEST_FPATH" $fpath); autoload -Uz compinit; compinit -D -i; [[ ${_comps[hostup]} == _hostup ]]' || fail 'Zsh autoload failed'
  echo 'PASS installer: Zsh discovers profile through compinit'
}

test_bash_autoload() {
  local framework=''
  for framework in /usr/share/bash-completion/bash_completion /etc/bash_completion; do
    if [ -s "$framework" ]; then break; fi
  done
  if [ ! -s "$framework" ]; then echo 'SKIP installer: Bash completion framework unavailable'; return; fi
  fixture
  run_installer install pass
  # Load only the framework, as normal distro startup does; it must discover
  # Hostup by command name using its standard XDG completion search path.
  HOME="$root/home" HOSTUP_TEST_FRAMEWORK="$framework" XDG_DATA_HOME="$root/home/data" \
    XDG_DATA_DIRS="$root/usr/share" BASH_COMPLETION_USER_FILE=/dev/null \
    BASH_COMPLETION_COMPAT_DIR="$root/compat" bash --noprofile --norc -c \
    'source "$HOSTUP_TEST_FRAMEWORK"; _completion_loader hostup; complete -p hostup' >/dev/null || fail 'Bash autoload failed'
  echo 'PASS installer: Bash framework discovers profile'
}

test_fish_autoload() {
  if ! command -v fish >/dev/null 2>&1; then echo 'SKIP installer: Fish runtime unavailable'; return; fi
  fixture; shell=fish
  run_installer install pass
  # Isolate user configuration and emulate the vendor directory in DESTDIR.
  HOME="$root/home" XDG_CACHE_HOME="$root/home/cache" \
    HOSTUP_TEST_BINARY="$binary" HOSTUP_TEST_FISH_PATH="$(dirname "$(profile fish)")" \
    XDG_CONFIG_HOME="$root/home/config" XDG_DATA_HOME="$root/home/data" \
    PATH="$root/usr/local/bin:$PATH" fish --no-config -c \
    'set fish_complete_path $HOSTUP_TEST_FISH_PATH $fish_complete_path; complete -C "hostup ser" | string match -q "service*"' || fail 'Fish autoload failed'
  echo 'PASS installer: Fish discovers profile'
}

main() {
  local project
  project=$(cd "${BASH_SOURCE[0]%/*}/.." && pwd)
  installer="$project/.dev/install.sh"
  work=$(mktemp -d)
  trap 'rm -rf "$work"' EXIT
  trap 'exit 130' INT
  trap 'exit 143' HUP TERM
  binary="$work/hostup"
  real_mv=$(command -v mv)
  (cd "$project" && go build -o "$binary" ./src)
  test_lifecycle
  test_detection
  test_ownership
  test_upgrade_and_failure
  test_zsh_autoload
  test_bash_autoload
  test_fish_autoload
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then main "$@"; fi
