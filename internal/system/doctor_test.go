package system

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseMemory(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    MemoryInfo
		wantErr string
	}{
		{name: "Linux meminfo", input: "MemTotal:       2048 kB\nMemFree: 10 kB\nMemAvailable:   1024 kB\nCached: 512 kB\n", want: MemoryInfo{TotalBytes: 2097152, AvailableBytes: 1048576}},
		{name: "different order and tabs", input: "MemAvailable:\t512\tkB\nMemTotal: 1024 kB", want: MemoryInfo{TotalBytes: 1048576, AvailableBytes: 524288}},
		{name: "zero available", input: "MemTotal: 1 kB\nMemAvailable: 0 kB", want: MemoryInfo{TotalBytes: 1024}},
		{name: "all available", input: "MemTotal: 1 kB\nMemAvailable: 1 kB", want: MemoryInfo{TotalBytes: 1024, AvailableBytes: 1024}},
		{name: "empty", wantErr: "missing MemTotal or MemAvailable"},
		{name: "total missing", input: "MemAvailable: 1 kB", wantErr: "missing"},
		{name: "available missing", input: "MemTotal: 1 kB", wantErr: "missing"},
		{name: "missing colon", input: "MemTotal 1 kB\nMemAvailable: 0 kB", wantErr: "missing"},
		{name: "wrong units", input: "MemTotal: 1 MB", wantErr: "invalid MemTotal"},
		{name: "missing units", input: "MemTotal: 1", wantErr: "invalid MemTotal"},
		{name: "extra tokens", input: "MemTotal: 1 kB extra", wantErr: "invalid MemTotal"},
		{name: "not numeric", input: "MemTotal: many kB", wantErr: "invalid MemTotal"},
		{name: "negative", input: "MemTotal: -1 kB", wantErr: "invalid MemTotal"},
		{name: "fractional", input: "MemTotal: 1.5 kB", wantErr: "invalid MemTotal"},
		{name: "invalid available", input: "MemTotal: 1 kB\nMemAvailable: invalid kB", wantErr: "invalid MemAvailable"},
		{name: "numeric overflow", input: "MemTotal: 18446744073709551616 kB", wantErr: "invalid MemTotal"},
		{name: "byte conversion overflow", input: "MemTotal: 18014398509481984 kB", wantErr: "overflows bytes"},
		{name: "zero total", input: "MemTotal: 0 kB\nMemAvailable: 0 kB", wantErr: "invalid memory totals"},
		{name: "available exceeds total", input: "MemTotal: 1 kB\nMemAvailable: 2 kB", wantErr: "invalid memory totals"},
		{name: "oversized line", input: strings.Repeat("x", 65536), wantErr: "read meminfo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMemory(strings.NewReader(tt.input))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				if got != (MemoryInfo{}) {
					t.Errorf("memory on error = %+v, want zero value", got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("parseMemory() = %+v, %v; want %+v, nil", got, err, tt.want)
			}
		})
	}
}

func TestParseMemoryReadError(t *testing.T) {
	wantErr := errors.New("reader failed")
	reader := io.MultiReader(strings.NewReader("MemTotal: 1024 kB\nMemAvailable: 512 kB\n"), failingReader{wantErr})
	if _, err := parseMemory(reader); !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapping %v", err, wantErr)
	}
}

func TestCollectDiagnostics(t *testing.T) {
	wantPlatform := Platform{OS: "linux", Distribution: "debian", Architecture: "arm64"}
	wantDisk := DiskInfo{Path: "/", TotalBytes: 4096, UsedBytes: 1024, AvailableBytes: 2048}
	tests := []struct {
		name    string
		meminfo string
		readErr error
		diskErr error
		wantErr string
		errIs   error
	}{
		{name: "success", meminfo: "MemTotal: 1024 kB\nMemAvailable: 512 kB"},
		{name: "read failure", readErr: os.ErrPermission, wantErr: "read memory information", errIs: os.ErrPermission},
		{name: "malformed memory", meminfo: "MemTotal: broken", wantErr: "parse memory information"},
		{name: "disk failure", meminfo: "MemTotal: 1024 kB\nMemAvailable: 512 kB", diskErr: os.ErrPermission, wantErr: "read root filesystem usage", errIs: os.ErrPermission},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diskCalled := false
			got, err := collectDiagnostics(wantPlatform, func(path string) ([]byte, error) {
				if path != "/proc/meminfo" {
					t.Fatalf("memory path = %q", path)
				}
				return []byte(tt.meminfo), tt.readErr
			}, func(path string) (DiskInfo, error) {
				diskCalled = true
				if path != "/" {
					t.Fatalf("disk path = %q", path)
				}
				return wantDisk, tt.diskErr
			})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				if tt.errIs != nil && !errors.Is(err, tt.errIs) {
					t.Errorf("error = %v, want wrapping %v", err, tt.errIs)
				}
				if got != (Diagnostics{}) {
					t.Errorf("report on error = %+v, want zero value", got)
				}
				if diskCalled != (tt.diskErr != nil) {
					t.Errorf("disk called = %t", diskCalled)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Platform != wantPlatform || got.Disk != wantDisk || got.Memory != (MemoryInfo{TotalBytes: 1048576, AvailableBytes: 524288}) || got.CPU.LogicalCPUs < 1 {
				t.Errorf("unexpected report: %+v", got)
			}
		})
	}
}

func TestDiagnoseUnsupportedHost(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("unsupported OS behavior is exercised on non-Linux hosts")
	}
	if _, err := Diagnose(); err == nil || !strings.Contains(err.Error(), "unsupported OS: "+runtime.GOOS) {
		t.Fatalf("Diagnose() error = %v", err)
	}
}

func TestReadDiskUsage(t *testing.T) {
	path := t.TempDir()
	got, err := readDiskUsage(path)
	if runtime.GOOS != "linux" {
		if err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Fatalf("readDiskUsage() error = %v", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != path || got.TotalBytes == 0 || got.UsedBytes > got.TotalBytes || got.AvailableBytes > got.TotalBytes {
		t.Errorf("invalid disk usage: %+v", got)
	}
	if _, err := readDiskUsage(filepath.Join(path, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing path error = %v, want os.ErrNotExist", err)
	}
}
