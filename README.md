# Hostup

**A lightweight, native-first Linux setup and management toolkit.**

Hostup aims to simplify Linux system configuration,
diagnostics, and everyday management through a unified
CLI and interactive terminal interface.

## Philosophy

- Native Linux tools first
- No mandatory background daemons
- No Docker or Ansible dependencies
- Minimal resource consumption
- Predictable and transparent operations
- Safe and repeatable configuration
- CLI and TUI backed by shared functionality

## Planned Platforms

- Ubuntu Server
- Debian
- Raspberry Pi OS

Target architectures: ARM64 and AMD64.

## Commands

```sh
hostup version
hostup info
hostup info --json
hostup doctor
hostup doctor --json
hostup service list [--json]
hostup service status <name> [--json]
hostup service start|stop|restart|enable|disable <name> [--yes]
hostup service logs <name> [--follow] [--lines N]
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

`info` and `doctor` require Linux. Development, unit tests, and cross-compilation
work on macOS; diagnostic tests use fixtures without Linux hardware.

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

Requirements:

- Go 1.24+
- Task (optional)

Build:

    go build -o bin/hostup ./cmd/hostup

Run:

    ./bin/hostup

Test:

    go test ./...

Verify and build for Linux ARM64 and AMD64:

    task ci

Task commands:

    task --list

### Linux integration testing on macOS

On Apple Silicon, install [Multipass](https://canonical.com/multipass/install),
Task, and jq. Multipass uses native ARM64 Ubuntu images. The default VM is
`hostup-sandbox`, running Ubuntu 24.04 LTS with 2 CPUs, 2G RAM, and a 10G disk.
Configure these defaults through the `VM_*` variables in `Taskfile.yml`.
VM lifecycle, integration tests, and installation logic live in `scripts/vm/`.
The scripts check for Multipass before invoking it, including when run directly;
lifecycle scripts also check for jq. Missing tools produce a clear `FAIL` message
and a nonzero exit code.
Each entry script groups its logic into shell functions and calls `main` only
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
task vm:uninstall        # Remove only the binary owned by this development workflow
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
This is a sandbox development installer; a public GitHub installer is not
implemented.

The installer records a checksum in the root-owned directory
`/var/lib/hostup-dev`. Repeated installs can update a matching managed binary.
Install and uninstall refuse an existing binary without a matching receipt,
a modified managed binary, unexpected ownership or permissions, and symlinks.
They never adopt an unrelated binary, even if its contents match. Installation
operations use a lock to prevent concurrent changes.

`task vm:test:install` installs the current build and verifies the receipt,
checksum, executable permissions, PATH resolution as the Ubuntu user, exact
`version` and `--version` output, help, diagnostics, and service listing.

```sh
task ci                   # Local checks first; no VM access
task vm:test              # Read-only CLI smoke checks after deployment
task vm:test:integration   # Changes only a disposable test service
task vm:test:install       # Exercise installation and the installed CLI
task vm:uninstall         # Remove the managed installed binary and its receipt
task vm:uninstall         # Safe to repeat
task vm:down              # Retain the VM and development deployment
task clean                # Remove local build artifacts
```

`vm:uninstall` requires an existing running sandbox; it does not create or start
one, and it leaves `/home/ubuntu/hostup` available for development. To remove
that deployment too, use `multipass exec hostup-sandbox -- rm -f /home/ubuntu/hostup`.
`task vm:delete` remains an explicit, confirmed permanent VM deletion; none of
the test or installation tasks delete or recreate the VM.

## Project Status

Early development.

The CLI implements `version`, `info`, `doctor`, and systemd service management.

## Documentation

Existing Raspberry Pi guides are preserved under:

`docs/guides/raspberry-pi/`

These are legacy reference materials and may require updates.

## License

To be determined.
