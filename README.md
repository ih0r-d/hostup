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

    task check
    task build:linux

Task commands:

    task --list

## Project Status

Early development.

The CLI implements `version`, `info`, `doctor`, and systemd service management.

## Documentation

Existing Raspberry Pi guides are preserved under:

`docs/guides/raspberry-pi/`

These are legacy reference materials and may require updates.

## License

To be determined.
