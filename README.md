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
```

`info` displays the Linux distribution, version, architecture, and hostname.
`doctor` reports the OS, logical CPU count, memory total and available,
and root filesystem total, used, and available space. Diagnostics are read-only
and require no root privileges or external commands.

Human-readable sizes use GiB; JSON sizes are integer bytes. Disk space available
excludes reserved filesystem blocks. If a diagnostic cannot be collected,
`doctor` returns an error and exits with a nonzero status.

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

The CLI implements `version`, `info`, and read-only `doctor` diagnostics.
System management features are not implemented yet.

## Documentation

Existing Raspberry Pi guides are preserved under:

`docs/guides/raspberry-pi/`

These are legacy reference materials and may require updates.

## License

To be determined.
