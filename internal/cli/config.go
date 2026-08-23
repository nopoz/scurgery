package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// configPath is defaultConfigPath behind a package variable so tests can point
// the loader at a file they wrote. Nothing outside the test binary can move it,
// which is what keeps it a test seam rather than a way to feed an operator's
// scurgery run someone else's credentials.
var configPath = defaultConfigPath

// The file may set these and nothing else. The closed set is the point: an
// unrecognised key is refused rather than ignored, so this file cannot quietly
// become the place an endpoint override lands.
var configKeys = map[string]bool{"TS_API_KEY": true, "TS_TAILNET": true}

func defaultConfigPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "scurgery", "config")
}

// loadConfig reads KEY=VALUE lines from path. A missing file is not an error:
// running without one is the normal case for anything automated.
func loadConfig(path string) (map[string]string, error) {
	cfg := map[string]string{}
	if path == "" {
		return cfg, nil
	}

	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if err := checkConfigMode(f, path); err != nil {
		return nil, err
	}

	scan := bufio.NewScanner(f)
	for line := 1; scan.Scan(); line++ {
		text := strings.TrimSpace(scan.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		key, value, ok := strings.Cut(text, "=")
		if !ok {
			return nil, fmt.Errorf("%s line %d: expected KEY=VALUE, got %q", path, line, text)
		}
		key = strings.TrimSpace(key)
		if !configKeys[key] {
			return nil, fmt.Errorf("%s line %d: unknown key %q; this file sets TS_API_KEY and TS_TAILNET only", path, line, key)
		}
		cfg[key] = unquote(strings.TrimSpace(value))
	}
	if err := scan.Err(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// checkConfigMode refuses a file anyone but its owner can read, the way ssh
// does. It holds an API token for a tool that rewrites access rules.
func checkConfigMode(f *os.File, path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		return fmt.Errorf("%s is readable by others (mode %04o); run: chmod 600 %s", path, mode, path)
	}
	return nil
}

// unquote strips one matched pair of surrounding quotes, so a token pasted
// with them still authenticates.
func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// credential resolves one name, environment first, so a config file on a
// build agent cannot quietly displace the credentials CI supplies.
func credential(name string, cfg map[string]string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return cfg[name]
}

// configHint names the config file in a credentials error, and says nothing
// when there is no such file to name: os.UserHomeDir fails in a container
// with no HOME, which is an ordinary place to be running this.
func configHint(path string) string {
	if path == "" {
		return ""
	}
	return fmt.Sprintf(" or put it in %s", path)
}
