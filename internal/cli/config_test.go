package cli

import (
	"bytes"
	"context"
	"net/http/httptest"

	"github.com/nopoz/scurgery/internal/api"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestMain keeps the package hermetic: no test may read the credentials of
// whoever happens to be running the suite. A test that needs a config file
// points configPath at one it wrote itself.
func TestMain(m *testing.M) {
	configPath = func() string { return filepath.Join(os.TempDir(), "scurgery-no-such-config") }
	os.Exit(m.Run())
}

func useConfig(t *testing.T, body string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	prev := configPath
	configPath = func() string { return path }
	t.Cleanup(func() { configPath = prev })
	return path
}

func TestLoadConfigReadsBothKeys(t *testing.T) {
	path := useConfig(t, "TS_API_KEY=tskey-api-abc\nTS_TAILNET=example.com\n", 0o600)

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg["TS_API_KEY"] != "tskey-api-abc" {
		t.Errorf("TS_API_KEY = %q, want %q", cfg["TS_API_KEY"], "tskey-api-abc")
	}
	if cfg["TS_TAILNET"] != "example.com" {
		t.Errorf("TS_TAILNET = %q, want %q", cfg["TS_TAILNET"], "example.com")
	}
}

func TestLoadConfigIgnoresCommentsAndBlankLines(t *testing.T) {
	path := useConfig(t, "# credentials\n\n  # indented comment\nTS_TAILNET = example.com \n\n", 0o600)

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg["TS_TAILNET"] != "example.com" {
		t.Errorf("TS_TAILNET = %q, want %q; whitespace around key and value is trimmed", cfg["TS_TAILNET"], "example.com")
	}
}

// A token pasted with the quotes still around it would otherwise fail
// authentication with a 401 that says nothing about the real cause.
func TestLoadConfigStripsMatchedSurroundingQuotes(t *testing.T) {
	for _, body := range []string{`TS_API_KEY="tskey-api-abc"`, `TS_API_KEY='tskey-api-abc'`} {
		path := useConfig(t, body+"\n", 0o600)
		cfg, err := loadConfig(path)
		if err != nil {
			t.Fatalf("loadConfig(%q): %v", body, err)
		}
		if cfg["TS_API_KEY"] != "tskey-api-abc" {
			t.Errorf("from %q: TS_API_KEY = %q, want %q", body, cfg["TS_API_KEY"], "tskey-api-abc")
		}
	}
}

func TestLoadConfigKeepsUnmatchedQuote(t *testing.T) {
	path := useConfig(t, `TS_API_KEY="tskey-api-abc`+"\n", 0o600)

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg["TS_API_KEY"] != `"tskey-api-abc` {
		t.Errorf("TS_API_KEY = %q, want the value unchanged: only a matched pair is stripped", cfg["TS_API_KEY"])
	}
}

// The key set is closed on purpose. Ignoring an unrecognised key would make
// this file the natural place for someone to later add an endpoint override,
// which is the surface this tool has already removed once.
func TestLoadConfigRejectsUnknownKey(t *testing.T) {
	path := useConfig(t, "TS_API_KEY=tskey-api-abc\nTS_BASE_URL=https://attacker.example\n", 0o600)

	_, err := loadConfig(path)
	if err == nil {
		t.Fatal("loadConfig should reject an unrecognised key, not ignore it")
	}
	if !strings.Contains(err.Error(), "TS_BASE_URL") {
		t.Errorf("error should name the offending key, got %v", err)
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error should name the line, got %v", err)
	}
}

func TestLoadConfigRejectsLineWithoutEquals(t *testing.T) {
	path := useConfig(t, "TS_API_KEY=tskey-api-abc\nnonsense\n", 0o600)

	_, err := loadConfig(path)
	if err == nil {
		t.Fatal("loadConfig should reject a line that is not KEY=VALUE")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error should name the line, got %v", err)
	}
}

// Not having a config file is the normal case for anyone automating, so it
// must not be an error.
func TestLoadConfigMissingFileIsNotAnError(t *testing.T) {
	cfg, err := loadConfig(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("a missing config file should not be an error, got %v", err)
	}
	if len(cfg) != 0 {
		t.Errorf("cfg = %v, want empty", cfg)
	}
}

func TestLoadConfigRefusesGroupOrWorldReadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits do not carry this meaning on Windows")
	}
	path := useConfig(t, "TS_API_KEY=tskey-api-abc\n", 0o644)

	_, err := loadConfig(path)
	if err == nil {
		t.Fatal("loadConfig should refuse a config file others can read")
	}
	if !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("error should say how to fix it, got %v", err)
	}
}

func TestDefaultConfigPathPrefersXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if got, want := defaultConfigPath(), filepath.Join("/xdg", "scurgery", "config"); got != want {
		t.Errorf("defaultConfigPath() = %q, want %q", got, want)
	}
}

func TestDefaultConfigPathFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/operator")
	if got, want := defaultConfigPath(), filepath.Join("/home/operator", ".config", "scurgery", "config"); got != want {
		t.Errorf("defaultConfigPath() = %q, want %q", got, want)
	}
}

// runCapturingCredentials runs a command against a fake tailnet and reports
// the token and tailnet Run resolved, which is what the precedence tests are
// actually about.
func runCapturingCredentials(t *testing.T, args []string) (code int, token, tailnet string, errb strings.Builder) {
	t.Helper()
	f := &fakeTailnet{policy: []byte(`{"grants": []}`), etag: `"e1"`, validateOK: true}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)

	orig := newClient
	newClient = func(tok, tn string) *api.Client {
		token, tailnet = tok, tn
		c := orig(tok, tn)
		c.BaseURL = srv.URL
		return c
	}
	t.Cleanup(func() { newClient = orig })

	var out bytes.Buffer
	code = Run(context.Background(), args, &out, &errb, strings.NewReader(""))
	return code, token, tailnet, errb
}

func TestRunUsesConfigFileWhenEnvironmentIsUnset(t *testing.T) {
	t.Setenv("TS_API_KEY", "")
	t.Setenv("TS_TAILNET", "")
	useConfig(t, "TS_API_KEY=tskey-from-file\nTS_TAILNET=file.example.com\n", 0o600)

	code, token, tailnet, errb := runCapturingCredentials(t, []string{"status"})
	if code != 0 {
		t.Fatalf("Run(status) = %d, want 0; stderr=%q", code, errb.String())
	}
	if token != "tskey-from-file" {
		t.Errorf("token = %q, want the one from the config file", token)
	}
	if tailnet != "file.example.com" {
		t.Errorf("tailnet = %q, want the one from the config file", tailnet)
	}
}

// The environment wins so CI and Terraform, where secrets arrive as
// environment variables, are unaffected by a config file being present.
func TestEnvironmentOverridesConfigFile(t *testing.T) {
	t.Setenv("TS_API_KEY", "tskey-from-env")
	t.Setenv("TS_TAILNET", "env.example.com")
	useConfig(t, "TS_API_KEY=tskey-from-file\nTS_TAILNET=file.example.com\n", 0o600)

	code, token, tailnet, errb := runCapturingCredentials(t, []string{"status"})
	if code != 0 {
		t.Fatalf("Run(status) = %d, want 0; stderr=%q", code, errb.String())
	}
	if token != "tskey-from-env" {
		t.Errorf("token = %q, want the environment to win over the config file", token)
	}
	if tailnet != "env.example.com" {
		t.Errorf("tailnet = %q, want the environment to win over the config file", tailnet)
	}
}

func TestTailnetFlagOverridesConfigFile(t *testing.T) {
	t.Setenv("TS_API_KEY", "")
	t.Setenv("TS_TAILNET", "")
	useConfig(t, "TS_API_KEY=tskey-from-file\nTS_TAILNET=file.example.com\n", 0o600)

	code, _, tailnet, errb := runCapturingCredentials(t, []string{"status", "--tailnet", "flag.example.com"})
	if code != 0 {
		t.Fatalf("Run(status) = %d, want 0; stderr=%q", code, errb.String())
	}
	if tailnet != "flag.example.com" {
		t.Errorf("tailnet = %q, want the flag to win over the config file", tailnet)
	}
}

// A config file that cannot be parsed must stop the run. Falling through to
// "TS_API_KEY is not set" would hide the real cause.
func TestRunReportsAnUnreadableConfigFile(t *testing.T) {
	t.Setenv("TS_API_KEY", "tskey-from-env")
	t.Setenv("TS_TAILNET", "env.example.com")
	useConfig(t, "TS_BASE_URL=https://attacker.example\n", 0o600)

	code, _, _, errb := runCapturingCredentials(t, []string{"status"})
	if code != 3 {
		t.Fatalf("Run(status) = %d, want 3 (runtime error); stderr=%q", code, errb.String())
	}
	if !strings.Contains(errb.String(), "TS_BASE_URL") {
		t.Errorf("stderr should explain what is wrong with the config file, got %q", errb.String())
	}
}

// Someone who has neither set the variables nor written the file needs to be
// told both ways out, including where the file goes.
func TestMissingCredentialsMentionTheConfigFile(t *testing.T) {
	t.Setenv("TS_API_KEY", "")
	t.Setenv("TS_TAILNET", "")
	absent := filepath.Join(t.TempDir(), "scurgery", "config")
	prev := configPath
	configPath = func() string { return absent }
	t.Cleanup(func() { configPath = prev })

	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"status"}, &out, &errb, strings.NewReader(""))
	if code != 3 {
		t.Fatalf("Run(status) = %d, want 3", code)
	}
	if !strings.Contains(errb.String(), absent) {
		t.Errorf("the error should name the config file as an alternative, got %q", errb.String())
	}
}

// A container with no HOME is an ordinary CI case. There is no config file to
// point at there, and a credentials error that trails off into an empty path
// is the kind of confusion this tool's error messages exist to avoid.
func TestCredentialErrorOmitsThePathWhenThereIsNone(t *testing.T) {
	t.Setenv("TS_API_KEY", "")
	t.Setenv("TS_TAILNET", "")
	prev := configPath
	configPath = func() string { return "" }
	t.Cleanup(func() { configPath = prev })

	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"status"}, &out, &errb, strings.NewReader(""))
	if code != 3 {
		t.Fatalf("Run(status) = %d, want 3", code)
	}
	if strings.Contains(errb.String(), "put it in") {
		t.Errorf("with no config path there is nowhere to put it; got %q", errb.String())
	}
	if !strings.Contains(errb.String(), "TS_API_KEY") {
		t.Errorf("the error should still name the variable, got %q", errb.String())
	}
}
