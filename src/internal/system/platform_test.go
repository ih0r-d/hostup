package system

import (
	"errors"
	"io"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestParseOSRelease(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]string
		wantErr string
	}{
		{name: "Ubuntu", input: "NAME=\"Ubuntu\"\nID=ubuntu\nVERSION_ID=\"24.04\"\nID_LIKE=debian\n", want: map[string]string{"NAME": "Ubuntu", "ID": "ubuntu", "VERSION_ID": "24.04", "ID_LIKE": "debian"}},
		{name: "Debian", input: "NAME='Debian GNU/Linux'\nID=debian\nVERSION_ID='12'", want: map[string]string{"NAME": "Debian GNU/Linux", "ID": "debian", "VERSION_ID": "12"}},
		{name: "Raspberry Pi OS", input: "PRETTY_NAME=\"Raspbian GNU/Linux 12 (bookworm)\"\nID=raspbian\nID_LIKE=debian\nVERSION_ID=12\n", want: map[string]string{"PRETTY_NAME": "Raspbian GNU/Linux 12 (bookworm)", "ID": "raspbian", "ID_LIKE": "debian", "VERSION_ID": "12"}},
		{name: "optional version absent", input: "ID=debian", want: map[string]string{"ID": "debian"}},
		{name: "unknown distribution preserved", input: "ID=custom-linux\nVENDOR_FIELD=value", want: map[string]string{"ID": "custom-linux", "VENDOR_FIELD": "value"}},
		{name: "ID punctuation and digits", input: "ID=custom_linux-1.0", want: map[string]string{"ID": "custom_linux-1.0"}},
		{name: "comments blanks whitespace CRLF", input: "\r\n # comment\r\n\tID = \"ubuntu\" \r\n VERSION_ID = 24.04 \r\n", want: map[string]string{"ID": "ubuntu", "VERSION_ID": "24.04"}},
		{name: "last duplicate wins", input: "ID=ubuntu\nID=debian", want: map[string]string{"ID": "debian"}},
		{name: "empty optional values", input: "ID=debian\nVERSION_ID=\nNAME=\"\"\nVARIANT=''", want: map[string]string{"ID": "debian", "VERSION_ID": "", "NAME": "", "VARIANT": ""}},
		{name: "equals and hash in quoted value", input: "ID=debian\nNAME=\"Linux = #1\"", want: map[string]string{"ID": "debian", "NAME": "Linux = #1"}},
		{name: "escaped shell characters", input: "ID=debian\n" + `NAME="A \"quote\" \\ \$HOME \` + "`" + `cmd\` + "`" + `"`, want: map[string]string{"ID": "debian", "NAME": "A \"quote\" \\ $HOME `cmd`"}},
		{name: "single quotes preserve backslash", input: "ID=debian\n" + `NAME='A "quote" \ path'`, want: map[string]string{"ID": "debian", "NAME": `A "quote" \ path`}},
		{name: "unknown double quote escape preserved", input: "ID=debian\n" + `NAME="Linux\q"`, want: map[string]string{"ID": "debian", "NAME": `Linux\q`}},
		{name: "unquoted escape", input: "ID=debian\n" + `NAME=Linux\ OS`, want: map[string]string{"ID": "debian", "NAME": "Linux OS"}},
		{name: "no variable expansion", input: "ID=debian\nNAME=\"$HOME\"", want: map[string]string{"ID": "debian", "NAME": "$HOME"}},
		{name: "UTF-8 name", input: "ID=debian\nNAME=\"Лінукс\"", want: map[string]string{"ID": "debian", "NAME": "Лінукс"}},
		{name: "empty file", wantErr: "missing ID"},
		{name: "comments only", input: "# ID=debian\n\n", wantErr: "missing ID"},
		{name: "ID missing", input: "NAME=Linux", wantErr: "missing ID"},
		{name: "ID empty", input: "ID=", wantErr: "missing ID"},
		{name: "ID double quoted empty", input: `ID=""`, wantErr: "missing ID"},
		{name: "ID single quoted empty", input: "ID=''", wantErr: "missing ID"},
		{name: "ID only whitespace", input: `ID=" "`, wantErr: "invalid ID"},
		{name: "ID uppercase", input: "ID=Debian", wantErr: "invalid ID"},
		{name: "ID embedded whitespace", input: `ID="debian linux"`, wantErr: "invalid ID"},
		{name: "ID path", input: "ID=/debian", wantErr: "invalid ID"},
		{name: "ID control character", input: "ID=debian\x00", wantErr: "invalid ID"},
		{name: "ID non-ASCII", input: "ID=лінукс", wantErr: "invalid ID"},
		{name: "duplicate clears ID", input: "ID=debian\nID=", wantErr: "missing ID"},
		{name: "missing equals", input: "ID=debian\nbroken", wantErr: "line 2: invalid assignment"},
		{name: "empty key", input: "ID=debian\n=value", wantErr: "invalid assignment"},
		{name: "key with whitespace", input: "ID=debian\nBAD KEY=value", wantErr: "invalid assignment"},
		{name: "key starts with digit", input: "ID=debian\n1KEY=value", wantErr: "invalid assignment"},
		{name: "key punctuation", input: "ID=debian\nBAD-KEY=value", wantErr: "invalid assignment"},
		{name: "unterminated double quote", input: `ID="debian`, wantErr: "unterminated quote"},
		{name: "unterminated single quote", input: "ID='debian", wantErr: "unterminated quote"},
		{name: "mismatched quotes", input: "ID=\"debian'", wantErr: "unterminated quote"},
		{name: "extra closing quote", input: `ID="debian""`, wantErr: "unexpected text"},
		{name: "quote in bare value", input: `ID=deb"ian`, wantErr: "unexpected whitespace or quote"},
		{name: "unquoted spaces", input: "ID=debian\nNAME=Debian Linux", wantErr: "unexpected whitespace or quote"},
		{name: "concatenated strings", input: `ID="deb"'ian'`, wantErr: "unexpected text"},
		{name: "trailing escape", input: `ID=debian\`, wantErr: "trailing escape"},
		{name: "escaped closing quote", input: `ID="debian\"`, wantErr: "unterminated quote"},
		{name: "oversized line", input: "ID=debian\nNAME=" + strings.Repeat("x", 65536), wantErr: "read os-release"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseOSRelease(strings.NewReader(tt.input))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				if got != nil {
					t.Fatalf("values on error = %v, want nil", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("values = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseOSReleaseReadError(t *testing.T) {
	wantErr := errors.New("reader failed")
	// Exercise an error after a valid ID, rather than just an empty reader.
	reader := io.MultiReader(strings.NewReader("ID=debian\n"), failingReader{wantErr})
	values, err := ParseOSRelease(reader)
	if !errors.Is(err, wantErr) || values != nil {
		t.Fatalf("ParseOSRelease() = %v, %v; want nil, %v", values, err, wantErr)
	}
}

func TestDetectPlatform(t *testing.T) {
	hostErr := errors.New("hostname unavailable")
	tests := []struct {
		name      string
		goos      string
		goarch    string
		files     map[string]string
		readErr   error
		hostErr   error
		want      Platform
		wantPaths []string
		wantErr   string
		errIs     error
	}{
		{name: "Ubuntu AMD64", goos: "linux", goarch: "amd64", files: map[string]string{"/etc/os-release": "ID=ubuntu\nVERSION_ID=24.04"}, want: Platform{"linux", "ubuntu", "24.04", "amd64", "hostup-test"}, wantPaths: []string{"/etc/os-release"}},
		{name: "Debian ARM64", goos: "linux", goarch: "arm64", files: map[string]string{"/etc/os-release": "ID=debian\nVERSION_ID=12"}, want: Platform{"linux", "debian", "12", "arm64", "hostup-test"}, wantPaths: []string{"/etc/os-release"}},
		{name: "Raspberry Pi OS ARM64", goos: "linux", goarch: "arm64", files: map[string]string{"/etc/os-release": "ID=raspbian\nVERSION_ID=12"}, want: Platform{"linux", "raspbian", "12", "arm64", "hostup-test"}, wantPaths: []string{"/etc/os-release"}},
		{name: "no optional version", goos: "linux", goarch: "arm64", files: map[string]string{"/etc/os-release": "ID=debian"}, want: Platform{"linux", "debian", "", "arm64", "hostup-test"}, wantPaths: []string{"/etc/os-release"}},
		{name: "macOS", goos: "darwin", goarch: "arm64", wantErr: "unsupported OS: darwin"},
		{name: "Windows", goos: "windows", goarch: "amd64", wantErr: "unsupported OS: windows"},
		{name: "missing files", goos: "linux", wantErr: "read os-release", errIs: os.ErrNotExist, wantPaths: []string{"/etc/os-release", "/usr/lib/os-release"}},
		{name: "fallback", goos: "linux", goarch: "amd64", files: map[string]string{"/usr/lib/os-release": "ID=debian"}, want: Platform{"linux", "debian", "", "amd64", "hostup-test"}, wantPaths: []string{"/etc/os-release", "/usr/lib/os-release"}},
		{name: "etc takes precedence", goos: "linux", goarch: "amd64", files: map[string]string{"/etc/os-release": "ID=ubuntu", "/usr/lib/os-release": "ID=debian\nVERSION_ID=12"}, want: Platform{"linux", "ubuntu", "", "amd64", "hostup-test"}, wantPaths: []string{"/etc/os-release"}},
		{name: "permission denied does not fall back", goos: "linux", readErr: os.ErrPermission, wantErr: "read os-release", errIs: os.ErrPermission, wantPaths: []string{"/etc/os-release"}},
		{name: "empty file does not fall back", goos: "linux", files: map[string]string{"/etc/os-release": "", "/usr/lib/os-release": "ID=debian"}, wantErr: "missing ID", wantPaths: []string{"/etc/os-release"}},
		{name: "malformed file does not fall back", goos: "linux", files: map[string]string{"/etc/os-release": `ID="debian`, "/usr/lib/os-release": "ID=debian"}, wantErr: "unterminated quote", wantPaths: []string{"/etc/os-release"}},
		{name: "malformed fallback", goos: "linux", files: map[string]string{"/usr/lib/os-release": "NAME=Linux"}, wantErr: "missing ID", wantPaths: []string{"/etc/os-release", "/usr/lib/os-release"}},
		{name: "hostname error", goos: "linux", files: map[string]string{"/etc/os-release": "ID=debian"}, hostErr: hostErr, wantErr: "get hostname", errIs: hostErr, wantPaths: []string{"/etc/os-release"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var paths []string
			hostCalled := false
			got, err := detectPlatform(tt.goos, tt.goarch, func(path string) ([]byte, error) {
				paths = append(paths, path)
				if tt.readErr != nil {
					return nil, tt.readErr
				}
				value, ok := tt.files[path]
				if !ok {
					return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
				}
				return []byte(value), nil
			}, func() (string, error) {
				hostCalled = true
				return "hostup-test", tt.hostErr
			})
			if !reflect.DeepEqual(paths, tt.wantPaths) {
				t.Errorf("read paths = %v, want %v", paths, tt.wantPaths)
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				if tt.errIs != nil && !errors.Is(err, tt.errIs) {
					t.Errorf("error = %v, want wrapping %v", err, tt.errIs)
				}
				if got != (Platform{}) {
					t.Errorf("platform on error = %+v, want zero value", got)
				}
				if hostCalled != (tt.hostErr != nil) {
					t.Errorf("hostname called = %t", hostCalled)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("platform = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDetectPlatformUnsupportedHost(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("unsupported OS behavior is exercised on non-Linux hosts")
	}
	if _, err := DetectPlatform(); err == nil || !strings.Contains(err.Error(), "unsupported OS: "+runtime.GOOS) {
		t.Fatalf("DetectPlatform() error = %v", err)
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }
