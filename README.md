# Hostup

**A safe, terminal-first Linux operations toolkit.**

Hostup makes system changes transparent, predictable, and verifiable through
native Linux integrations shared by CLI and TUI. Hostname and timezone setup
follows inspect → plan → validate → explicitly approve → apply → verify.

## Philosophy

- Native Linux tools first
- No mandatory background daemons
- No Docker or Ansible dependencies
- Minimal resource consumption
- Predictable and transparent operations
- Safe and repeatable configuration
- Explicit approval and verification of actual system state
- CLI and TUI backed by shared functionality

## Planned Platforms

- Ubuntu Server
- Debian
- Raspberry Pi OS

Target architectures: ARM64 and AMD64.

## Commands

```sh
hostup
hostup tui
hostup version
hostup info
hostup info --json
hostup doctor
hostup doctor --json
hostup service list [--json]
hostup service status <name> [--json]
hostup service start|stop|restart|enable|disable <name> [--yes]
hostup service logs <name> [--follow] [--lines N]
hostup setup plan [--hostname NAME] [--timezone ZONE] [--json]
hostup setup apply [--hostname NAME] [--timezone ZONE] [--yes]
hostup setup wizard
hostup completion bash|zsh|fish
```

`info` displays the Linux distribution, version, architecture, and hostname.
`doctor` reports the OS, logical CPU count, memory total and available,
and root filesystem total, used, and available space. Diagnostics are read-only
and require no root privileges or external commands.

Human-readable sizes use GiB; JSON sizes are integer bytes. Disk space available
excludes reserved filesystem blocks. If a diagnostic cannot be collected,
`doctor` returns an error and exits with a nonzero status.

Service management uses `systemctl` and `journalctl` directly on Linux systems
running systemd. Changes prompt for confirmation on a terminal; use `--yes` for
automation. Hostup never invokes `sudo`.
Service changes disable native password prompts and have a 30-second deadline.
Finite service queries/log reads are bounded; explicit log following runs until
cancelled. Existing service commands report execution results, without setup's
plan/verification lifecycle.

`info` and `doctor` require Linux. Development, unit tests, and cross-compilation
work on macOS; diagnostic tests use fixtures without Linux hardware.

### Interactive dashboard

`hostup` opens the main TUI when both stdin and stdout are interactive terminals.
With redirected input/output it prints concise command usage and exits successfully.
`hostup tui` requires interactive terminals and returns an error otherwise;
`TERM=dumb` is unsupported. Explicit CLI commands never open the dashboard.

The navigation order is Overview, System Setup, Services, Network, VPN, Security,
Storage, Packages, Logs. Overview is initially selected. **Up/Down** or **j/k**
select a section; **Enter** opens it. Moving the selection performs no backend
work. **Esc** returns to navigation or the parent view, **q** quits navigation,
and **Ctrl+C** exits safely from any screen. **Tab** changes focus only inside
the wizard's configuration fields, where multiple controls exist.

Overview reads an unprivileged Linux diagnostic snapshot on entry: hostname,
distribution/version, architecture, logical CPUs, available/total RAM, used/total
disk, and diagnostic collection status. It does not report invented CPU usage,
health checks, service counts, or live metrics. There is no polling.

Services loads a scrollable, read-only systemd list. **Enter** reads the selected
unit's load, active, and substate using the same status backend as the CLI.
**Esc** preserves selection when returning from details. Service changes and
logs remain in the existing CLI commands.

System Setup runs the existing wizard inside the same Bubble Tea event loop.
Closing or cancelling the wizard with **Esc** returns to Overview. Explicit
confirmation remains mandatory for changes. **Ctrl+C** during application waits
for the active operation and leaves a partial/cancellation summary after exit.
The standalone `hostup setup wizard` command remains available.

Network, VPN, Security, Storage, Packages, and Logs show **Not implemented yet**.
VPN lists planned providers without operational statuses or actions. No backend
operations exist for these placeholders.

The original logo, navy surfaces, cyan focus, and green successful states follow
the references in `src/assets/mokups/`. The sidebar collapses on narrow screens;
content and controls remain within the viewport. The same fixed navy/cyan/green palette is used on light- and dark-background
terminals. Colors adapt to 256/16-color capabilities; `NO_COLOR` and colorless
terminals use monochrome. The entire viewport is painted, including unused space. Below 24 columns or 12 rows,
resize the terminal to use the dashboard. Overview and Services report Linux
limitations clearly on macOS; navigation and placeholders remain usable.

### Initial setup

Preview and apply hostname/timezone configuration on Linux:

```sh
hostup setup plan --hostname pi-server --timezone Europe/Kyiv
hostup setup plan --hostname pi-server --timezone Europe/Kyiv --json
hostup setup apply --hostname pi-server --timezone Europe/Kyiv
# Automation requires explicit confirmation; run with privileges yourself.
hostup setup apply --hostname pi-server --timezone Europe/Kyiv --yes
hostup setup plan                    # Show current settings; change nothing
hostup setup apply --timezone UTC    # Keep the current hostname
```

Plans are strictly read-only. Text and JSON report `current`, `desired`, and
whether each value is `changed`; text labels unchanged settings explicitly.
Omitted flags retain current values; explicitly empty values are invalid.
Hostnames use lowercase DNS labels (letters, digits, and internal hyphens),
with at most 63 bytes per label and 64 bytes total. Timezones must be named
locations in the available IANA timezone database, such as `UTC`, `Europe/Kyiv`,
or `America/New_York`; process-local `Local` and numeric offsets are unsupported.
Timezone identifiers are compared exactly: `UTC` and `Etc/UTC` remain distinct;
Hostup does not normalize aliases or infer equivalence from offsets.

Hostup reads the kernel hostname and the `/etc/localtime` zoneinfo symlink.
If that link cannot identify a timezone, it queries `timedatectl` read-only.
It validates all supplied values before making changes. `apply` prints the plan
and prompts on an interactive terminal; non-interactive execution requires
`--yes`, including when nothing needs changing. Apply rereads current settings
after confirmation and rejects unrelated changes since planning.

Only necessary changes run, in hostname then timezone order, through
`hostnamectl --no-ask-password set-hostname` and
`timedatectl --no-ask-password set-timezone`, without a shell or automatic sudo.
These native tools persist the system settings; Hostup adds no configuration,
receipt, or state files. The system must provide the native systemd tools and
appropriate permissions. Setup inspection and each external command have a 30-second timeout and
honor the caller's context cancellation.

After each successful setup command, Hostup rereads actual hostname and timezone.
`Applied` now means observed at the desired value, rather than merely a zero
command exit status. Successful commands without a matching observation are
reported as `Executed (not verified)`. A mismatch or unavailable observation
returns an error and stops further changes. Earlier settings are rechecked in
each successful snapshot, including settings retained without changes.

Application stops on the first error and reports verified settings, the failed
setting, and changes not attempted. A failed or interrupted command may already
have changed its setting; rerun `setup plan` before retrying. Retry always
reinspects actual state and skips settings already at their desired values.
There is no automatic retry or rollback. Inspections are snapshots, not an atomic
transaction: another process can change settings after preflight or verification.
An unreadable final snapshot leaves resulting state uncertain; earlier verified
settings describe the last successful observation, not a guarantee of permanence.

The reusable `src/internal/setup` engine owns validation, planning, execution, and verification.
Cobra handles flags, text/JSON rendering, and confirmation; the interactive
wizard uses the same engine and validators. `ApplyWithProgress` reports operation
boundaries without exposing command output; `Applied` progress follows read-back.
`Apply` retains its existing API and plan JSON fields are unchanged. Structured
results distinguish successful commands (`Executed`), verified values (`Applied`),
retained values, failed settings, and unattempted changes. Verification errors
distinguish mismatches from unavailable observations while preserving their causes.

### Interactive setup wizard

```sh
hostup setup wizard
NO_COLOR=1 hostup setup wizard
```

The wizard requires both stdin and stdout to be interactive terminals. Open it
through System Setup in the dashboard or explicitly with `hostup setup wizard`.
Configure settings inside the wizard; `--hostname`, `--timezone`, `--json`,
and `--yes` belong to the non-interactive plan/apply workflow.

The centered layout follows the supplied setup design: cyan active steps, navy
panels, shaded comparison rows, green results, and a separate action bar. Wide
terminals show a navigation rail containing only the implemented System Setup
section. The existing PNG logo is embedded and rendered as a compact terminal
mark; narrow and monochrome layouts use a simple wordmark fallback. The fixed dark Hostup palette is shared with the dashboard, independent of
terminal appearance; low-color and monochrome terminals have fallbacks.

The welcome screen shows the platform. Configuration fields start with the
current values, preserve unedited settings, and show validation
errors inline. Continue refreshes the read-only plan. Review marks each setting
as **CHANGE** or **SAME**; press **Space** to confirm actual changes, then **Enter**
to apply. Enter alone cannot authorize changes. If nothing changes, review shows
**Already configured**: **Enter** verifies the snapshot and closes, without
confirmation or any state-changing commands.

Controls:

- **Tab / Shift+Tab**: switch configuration fields.
- **Enter**: Continue, Apply after confirmation, or Done on a no-op/results screen.
- **Esc**: Back before applying; Close on results.
- **Ctrl+C**: cancel. During application, wait for the active operation to return
  so the final summary preserves completed, failed, and unattempted changes.
- **PgUp / PgDn**: scroll content while controls remain visible.
- **d**: toggle diagnostic details on an error/result screen.

The footer shows only controls relevant to the current screen, adding scroll
hints when content overflows. Below 32 columns or 14 rows the layout asks
for more space and prevents hidden confirmation/application actions. `NO_COLOR`
uses monochrome presentation; state labels remain meaningful without colors.
Progress reports completion only after an operation is applied and verified. Failures do not
retry automatically or roll back changes. Results remain visible until closed,
and the standalone wizard leaves a plain summary in the terminal afterward.

Setup changes require Linux with native systemd tools and appropriate privileges.
On macOS the welcome screen reports the unsupported environment; no changes can
be applied. The wizard never elevates privileges. Insufficient permissions are
reported directly, with raw diagnostics hidden until requested.

## Planned Features

- Initial system setup
- System diagnostics
- Network configuration
- SSH configuration
- VPN management
- systemd service management
- Storage management
- System updates and maintenance

## Development

Application sources and embedded resources live under `src/`; development tools
live under `.dev/`. The Go module stays at the repository root.

```text
src/
  main.go           # CLI entry point and process exit handling
  internal/
    cli/            # Cobra commands and completion
    setup/          # Shared setup engine
    system/         # Native Linux operations
    tui/            # Main application, navigation, and read-only screens
      setup/        # Existing wizard and embedded session adapter
  assets/           # Brand resources; only the logo is embedded
    mokups/         # Design references, excluded from the binary
.dev/
  Taskfile.*.yml     # Build, local development, and Multipass tasks
  install.sh        # Local binary and completion lifecycle
  install_test.sh   # Isolated shell tests
  vm/               # Multipass helpers
```

Requirements:

- Go 1.26+
- Task (optional)

Build:

    go build -o bin/hostup ./src

Run:

    ./bin/hostup

Test:

    go test ./...
    bash .dev/install_test.sh

The shell tests build the local CLI and exercise installation, reinstallation,
shell detection, rollback, and uninstall safety inside temporary `DESTDIR` trees.
They run without Linux, root, or a VM. Runtime autoload checks run when the
corresponding shell/framework is available; unavailable checks print `SKIP`.

Verify and build for Linux ARM64 and AMD64:

    task ci

Task commands:

    task --list

The development toolset is split into small Taskfiles; command names stay the same:

| File | Responsibility |
| --- | --- |
| `Taskfile.yml` | Includes, version discovery, and `task dev` / `task ci` workflows |
| `.dev/Taskfile.go.yml` | Shared build defaults, run, build, formatting, tests, and cleanup |
| `.dev/Taskfile.local.yml` | Development symlink installation and removal |
| `.dev/Taskfile.vm.yml` | Multipass configuration, lifecycle, deployment, and VM tests |

Use `task dev` for local development checks and a build, `task run -- --help` to
run the CLI, and `task ci` for complete verification. `task build`,
`task dev:install`, and all `task vm:*` commands remain available.

### Linux integration testing on macOS

On Apple Silicon, install [Multipass](https://canonical.com/multipass/install),
Task, and jq. Multipass uses native ARM64 Ubuntu images. The default VM is
`hostup-sandbox`, running Ubuntu 24.04 LTS with 2 CPUs, 2G RAM, and a 10G disk.
Configure these defaults through the `VM_*` variables in `.dev/Taskfile.vm.yml`
or pass overrides, for example `task vm:up VM_MEMORY=4G`.
VM lifecycle and integration tests live in `.dev/vm/`; installation reuses
`.dev/install.sh`.
The scripts check for Multipass before invoking it, including when run directly;
lifecycle scripts also check for jq. Missing tools produce a clear `FAIL` message
and a nonzero exit code.
Each VM entry script groups its logic into shell functions and calls `main` only
when executed directly. Sourcing a script defines functions without contacting
the VM; guest operations are also organized into functions with their own `main`.

```sh
task vm:up      # Create if missing, start if stopped; reuse a running VM
task vm:status  # Show VM state and information
task vm:shell   # Ensure the VM is running and open an Ubuntu shell
task vm:deploy  # Rebuild Linux ARM64 and copy to /home/ubuntu/hostup
task vm:test    # Deploy, then run read-only smoke tests
task vm:test:integration  # Deploy, then exercise a disposable systemd service
task vm:install          # Deploy and install at /usr/local/bin/hostup
task vm:test:install     # Install, then verify ownership, permissions, PATH and CLI
task vm:uninstall        # Remove the managed binary and shell completion
task vm:down    # Stop the VM and retain its disk
task vm:delete  # Confirm permanent deletion of only this VM
```

Smoke tests run `version`, `info`, `doctor`, `service list`, and status/log
checks for `systemd-journald`. They stop with a nonzero exit code on any failure
and do not change systemd services. `vm:deploy` and `vm:test` ensure the VM is
running and rebuild the binary on every invocation.

Run `task ci` for local verification without creating or changing any VMs.

### Systemd integration tests

`task vm:test:integration` runs with explicit root privileges inside the sandbox.
It creates a uniquely named `hostup-integration.*.service`, then tests service
list, status (text and JSON), start, stop, restart, enable, disable, and logs.
Each operation is checked against actual systemd state; restart also checks
that the main PID changed. The suite verifies inactive and deliberately failed
service status. All Hostup changes pass `--yes` and target only the test unit.

The exit trap stops and disables the unit, clears its failed state, removes
the unit and temporary files, and reloads systemd on success or failure,
including catchable interruption signals. Each run uses a new unit name.
Journal entries remain subject to the VM's normal journal retention; the suite
does not purge shared system logs. An uncatchable kill or VM crash can prevent
cleanup; inspect any leftover `hostup-integration.*.service` before removing
that exact unit through `task vm:shell`.

Tests print concise `PASS` lines. Failures return a nonzero exit code and show
the failing step, command output, unit status, and recent journal entries.
No production or critical system services are changed.

### Installation lifecycle testing

`vm:deploy` copies an executable to `/home/ubuntu/hostup` for direct development
testing. It does not put Hostup on PATH. `vm:install` additionally installs a
root-owned executable with mode `0755` at `/usr/local/bin/hostup`, using the
locally built binary. It preserves system configuration and shell startup files.
It also generates one completion profile for the detected shell. This local
development installer does not download binaries; a public GitHub installer is
not implemented. `task dev:install` is a development symlink only and does not
install system-wide completion.

The installer records binary and completion checksums in the root-owned directory
`/var/lib/hostup-dev`. Repeated installs can update matching managed files.
Install and uninstall refuse an existing binary without a matching receipt,
a modified managed binary, unexpected ownership or permissions, and symlinks.
They never adopt an unrelated binary, even if its contents match. Installation
operations use a lock to prevent concurrent changes.

`task vm:test:install` installs the current build and verifies the receipt,
checksum, executable permissions, PATH resolution as the Ubuntu user, exact
`version` and `--version` output, help, diagnostics, service listing, and completion
discovery in a fresh shell. A missing Bash completion framework makes that
verification fail with installation guidance.

```sh
task ci                   # Local checks first; no VM access
task vm:test              # Read-only CLI smoke checks after deployment
task vm:test:integration   # Changes only a disposable test service
task vm:test:install       # Exercise installation and the installed CLI
task vm:uninstall         # Remove the managed binary, completion and receipts
task vm:uninstall         # Safe to repeat
task vm:down              # Retain the VM and development deployment
task clean                # Remove local build artifacts
```

`vm:uninstall` requires an existing running sandbox; it does not create or start
one, and it leaves `/home/ubuntu/hostup` available for development. To remove
that deployment too, use `multipass exec hostup-sandbox -- rm -f /home/ubuntu/hostup`.
`task vm:delete` remains an explicit, confirmed permanent VM deletion; none of
the test or installation tasks delete or recreate the VM.

### Automatic shell completion

On Linux, installation detects the invoking Bash, Zsh, or Fish process, looking
through launcher/sudo parents. If no supported parent is found, it falls back to
the invoking user's login shell, then `$SHELL` when account lookup is unavailable.
Only one shell profile is generated through Cobra's existing `hostup completion`
command:

| Shell | System-wide completion file |
| --- | --- |
| Bash | `/usr/share/bash-completion/completions/hostup.bash` |
| Zsh | `/usr/local/share/zsh/site-functions/_hostup` |
| Fish | `/usr/share/fish/vendor_completions.d/hostup.fish` |

Open a new session of that shell after installation; no `source` command or
shell configuration edits are needed with normal distribution completion support.
Bash needs `bash-completion`: if it is missing, the installer prints guidance to
run `sudo apt install bash-completion` on Ubuntu. It never installs packages or
edits startup files itself. Zsh needs its normal `compinit` startup; custom setups
that disable completion or replace completion search paths must enable their
shell's standard completion support themselves.

Reinstall from a different shell to switch the single managed profile. Only
checksum-verified Hostup profiles are replaced or removed; existing unowned or
modified files are preserved and conflicts fail clearly. Uninstall uses receipts
and works independently of the current shell.

For a local Ubuntu build, run these commands explicitly with privileges:

```sh
task build
sudo bash .dev/install.sh install "$PWD/bin/hostup"
# Open a new session, then try Tab after these prefixes:
hostup ser
hostup service status ssh
hostup setup plan --timezone Europe/
sudo bash .dev/install.sh uninstall
```

Completion covers commands, subcommands, flags, loaded and installed systemd
services, and timezone values from local zone tables (with a read-only
`timedatectl` fallback). System queries have a short deadline, do not prompt or
invoke sudo, and return gracefully when tools are missing. Completion never
applies setup or service changes. `task ci` includes isolated installer tests;
`task vm:test:install` checks real Ubuntu installation when explicitly invoked.

## Project Status

Early development.

The CLI implements `version`, `info`, `doctor`, systemd service management,
and hostname/timezone setup planning, application, and an interactive wizard.

## Documentation

Existing Raspberry Pi guides are preserved under:

`docs/guides/raspberry-pi/`

These are legacy reference materials and may require updates.

## License

To be determined.
