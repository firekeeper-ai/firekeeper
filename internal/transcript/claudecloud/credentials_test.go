package claudecloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

var credNow = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

// loginJSON is the shape Claude Code stores in .credentials.json and the
// macOS keychain item. Every value is synthetic.
func loginJSON(token string, expires time.Time) string {
	exp := ""
	if !expires.IsZero() {
		exp = `,"expiresAt":` + strconv.FormatInt(expires.UnixMilli(), 10)
	}
	return `{"claudeAiOauth":{"accessToken":"` + token + `","refreshToken":"refresh-not-read"` + exp +
		`,"scopes":["user:inference"],"subscriptionType":"max"}}`
}

const orgConfig = `{"oauthAccount":{"organizationUuid":"` + testOrg + `"},"projects":{}}`

type credHome struct {
	home string
	env  map[string]string
}

func newCredHome(t *testing.T, creds, config string, mode os.FileMode) credHome {
	t.Helper()
	home := t.TempDir()
	if config != "" {
		if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if creds != "" {
		dir := filepath.Join(home, ".claude")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, ".credentials.json")
		if err := os.WriteFile(path, []byte(creds), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	return credHome{home: home, env: map[string]string{}}
}

func (h credHome) load(goos string, keychain func(context.Context) ([]byte, error)) (Credentials, error) {
	if keychain == nil {
		keychain = func(context.Context) ([]byte, error) { return nil, errors.New("no keychain item") }
	}
	return LoadCredentialsWith(context.Background(), h.home, CredentialOptions{
		GOOS:     goos,
		Getenv:   func(k string) string { return h.env[k] },
		Keychain: keychain,
		Now:      func() time.Time { return credNow },
	})
}

func TestCredentialsFileOnLinuxAndWindows(t *testing.T) {
	future := credNow.Add(time.Hour)
	tests := []struct {
		name   string
		creds  string
		config string
		mode   os.FileMode
		want   error
	}{
		{"valid", loginJSON(testToken, future), orgConfig, 0o600, nil},
		{"valid without expiry", loginJSON(testToken, time.Time{}), orgConfig, 0o600, nil},
		{"loose permissions are tolerated", loginJSON(testToken, future), orgConfig, 0o644, nil},
		{"missing file", "", orgConfig, 0o600, ErrNoLogin},
		{"expired", loginJSON(testToken, credNow.Add(-time.Minute)), orgConfig, 0o600, ErrTokenExpired},
		{"expires within the skew", loginJSON(testToken, credNow.Add(30*time.Second)), orgConfig, 0o600, ErrTokenExpired},
		{"malformed JSON", `{"claudeAiOauth":`, orgConfig, 0o600, ErrNoLogin},
		{"no oauth object", `{"other":{"accessToken":"x"}}`, orgConfig, 0o600, ErrNoLogin},
		{"empty token", loginJSON("", future), orgConfig, 0o600, ErrNoLogin},
		{"expiresAt of the wrong type is ignored", `{"claudeAiOauth":{"accessToken":"` + testToken + `","expiresAt":"soon"}}`, orgConfig, 0o600, nil},
		{"no organization", loginJSON(testToken, future), "", 0o600, ErrNoOrganization},
		{"organization unsafe in a header", loginJSON(testToken, future), `{"oauthAccount":{"organizationUuid":"a\r\nX: y"}}`, 0o600, ErrNoOrganization},
		{"organization is a device name", loginJSON(testToken, future), `{"oauthAccount":{"organizationUuid":"CON"}}`, 0o600, ErrNoOrganization},
	}
	for _, goos := range []string{"linux", "windows"} {
		for _, tc := range tests {
			t.Run(goos+"/"+tc.name, func(t *testing.T) {
				h := newCredHome(t, tc.creds, tc.config, tc.mode)
				keychainCalled := false
				creds, err := h.load(goos, func(context.Context) ([]byte, error) {
					keychainCalled = true
					return []byte(loginJSON("keychain-token", time.Time{})), nil
				})
				if keychainCalled {
					t.Fatal("the keychain is read only on macOS")
				}
				if !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want %v", err, tc.want)
				}
				if err != nil {
					if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), h.home) {
						t.Fatalf("error leaks a secret or path: %v", err)
					}
					return
				}
				if creds.token != testToken || creds.OrgUUID != testOrg {
					t.Fatal("wrong credentials")
				}
			})
		}
	}
}

func TestCredentialsFileIsNeverWritten(t *testing.T) {
	body := loginJSON(testToken, credNow.Add(-time.Hour))
	h := newCredHome(t, body, orgConfig, 0o600)
	path := filepath.Join(h.home, ".claude", ".credentials.json")
	before, _ := os.Stat(path)
	if _, err := h.load("linux", nil); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("err = %v", err)
	}
	after, _ := os.Stat(path)
	got, _ := os.ReadFile(path)
	if string(got) != body || !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("the credentials file changed; it must be read-only to Firekeeper")
	}
	if !strings.Contains(ErrTokenExpired.Error(), "run `claude` once") {
		t.Fatalf("expired message must tell the user how to refresh: %q", ErrTokenExpired)
	}
}

func TestCredentialsUnreadableFile(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("file modes do not deny reads here")
	}
	h := newCredHome(t, loginJSON(testToken, time.Time{}), orgConfig, 0o000)
	if _, err := h.load("linux", nil); !errors.Is(err, ErrUnreadableLogin) {
		t.Fatalf("err = %v, want ErrUnreadableLogin", err)
	}
}

func TestCredentialsHonorClaudeConfigDir(t *testing.T) {
	h := newCredHome(t, "", orgConfig, 0o600)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(loginJSON(testToken, time.Time{})), 0o600); err != nil {
		t.Fatal(err)
	}
	h.env[envConfigDir] = dir
	creds, err := h.load("linux", nil)
	if err != nil || creds.token != testToken || creds.OrgUUID != testOrg {
		t.Fatalf("err = %v; CLAUDE_CONFIG_DIR's credentials and ~/.claude.json's org must be used", err)
	}
	other := "99999999-2222-3333-4444-555555555555"
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"oauthAccount":{"organizationUuid":"`+other+`"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if creds, err = h.load("linux", nil); err != nil || creds.OrgUUID != other {
		t.Fatalf("err = %v; CLAUDE_CONFIG_DIR/.claude.json wins over ~/.claude.json", err)
	}
}

func TestCredentialsOnMacOS(t *testing.T) {
	future := credNow.Add(time.Hour)
	t.Run("keychain first", func(t *testing.T) {
		h := newCredHome(t, loginJSON("file-token", future), orgConfig, 0o600)
		creds, err := h.load("darwin", func(context.Context) ([]byte, error) { return []byte(loginJSON(testToken, future) + "\n"), nil })
		if err != nil || creds.token != testToken {
			t.Fatalf("err = %v; the keychain item wins on macOS", err)
		}
	})
	t.Run("expired keychain item", func(t *testing.T) {
		h := newCredHome(t, "", orgConfig, 0o600)
		_, err := h.load("darwin", func(context.Context) ([]byte, error) {
			return []byte(loginJSON(testToken, credNow.Add(-time.Hour))), nil
		})
		if !errors.Is(err, ErrTokenExpired) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("file when no keychain item", func(t *testing.T) {
		h := newCredHome(t, loginJSON(testToken, future), orgConfig, 0o600)
		creds, err := h.load("darwin", func(context.Context) ([]byte, error) { return nil, errors.New("boom " + testToken) })
		if err != nil || creds.token != testToken {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("nothing", func(t *testing.T) {
		h := newCredHome(t, "", orgConfig, 0o600)
		_, err := h.load("darwin", func(context.Context) ([]byte, error) { return []byte(`{}`), nil })
		if !errors.Is(err, ErrNoLogin) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestCredentialsKeychainTimeout(t *testing.T) {
	h := newCredHome(t, loginJSON("file-token", time.Time{}), orgConfig, 0o600)
	opts := CredentialOptions{
		GOOS:   "darwin",
		Getenv: func(string) string { return "" },
		Keychain: func(ctx context.Context) ([]byte, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	start := time.Now()
	_, err := loadCredentialsWithTimeout(context.Background(), h.home, opts, 30*time.Millisecond)
	if !errors.Is(err, ErrKeychainTimeout) {
		t.Fatalf("err = %v, want keychain timeout instead of file fallback", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("keychain timeout did not bound the lookup")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = loadCredentialsWithTimeout(ctx, h.home, opts, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want caller cancellation", err)
	}
}

func TestKeychainCommandHelper(t *testing.T) {
	switch os.Getenv("FIREKEEPER_TEST_KEYCHAIN_HELPER") {
	case "output":
		fmt.Print("synthetic-keychain-output")
		os.Exit(0)
	case "block":
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
}

func TestKeychainCommandCancellation(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-test.run=^TestKeychainCommandHelper$"}

	t.Setenv("FIREKEEPER_TEST_KEYCHAIN_HELPER", "output")
	out, err := runKeychainCommand(context.Background(), exe, args...)
	if err != nil || string(out) != "synthetic-keychain-output" {
		t.Fatalf("keychain helper failed: %v", err)
	}

	t.Setenv("FIREKEEPER_TEST_KEYCHAIN_HELPER", "block")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := runKeychainCommand(ctx, exe, args...); err == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("err = %v, context error = %v", err, ctx.Err())
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("cancelled keychain process did not stop promptly")
	}
}

func TestCredentialsTokenCannotBeSerialized(t *testing.T) {
	creds := Credentials{token: testToken, OrgUUID: testOrg}
	data, err := json.Marshal(creds)
	if err != nil || strings.Contains(string(data), testToken) {
		t.Fatalf("json = %s, err %v", data, err)
	}
	for _, s := range []string{fmt.Sprint(creds), fmt.Sprintf("%+v", creds), fmt.Sprintf("%#v", creds)} {
		if strings.Contains(s, testToken) {
			t.Fatalf("formatted credentials leak the token: %q", s)
		}
	}
}

func TestKeychainLookup(t *testing.T) {
	if securityPath != "/usr/bin/security" {
		t.Fatalf("security is run from %q, want the absolute system path", securityPath)
	}

	if got := strings.Join(keychainArgs("dan"), " "); got != "find-generic-password -a dan -s Claude Code-credentials -w" {
		t.Fatalf("args = %q", got)
	}
	// No account name: match the service alone rather than pass "-a ''".
	if got := strings.Join(keychainArgs(""), " "); got != "find-generic-password -s Claude Code-credentials -w" {
		t.Fatalf("args = %q", got)
	}
	t.Setenv("USER", "  someone ")
	if got := keychainAccount(); got != "someone" {
		t.Fatalf("account = %q", got)
	}
	t.Setenv("USER", "")
	if u, err := user.Current(); err == nil && keychainAccount() != strings.TrimSpace(u.Username) {
		t.Fatalf("empty $USER did not fall back to the OS user (%q)", keychainAccount())
	}
}

func TestSafeIDRejectsWindowsDevices(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"session_01TEST", true},
		{"console", true},
		{"COM10", true},
		{"com1_extra", true},
		{strings.Repeat("x", 128), true},
		{"CON", false},
		{"con", false},
		{"PrN", false},
		{"AUX", false},
		{"nul", false},
		{"COM0", false},
		{"cOm1", false},
		{"COM9", false},
		{"LPT0", false},
		{"lPt9", false},
		{"COM¹", false},
		{"LPT²", false},
		{"", false},
		{"../CON", false},
		{"CON.txt", false},
		{strings.Repeat("x", 129), false},
	}
	for _, tc := range tests {
		if got := safeID(tc.id); got != tc.want {
			t.Errorf("safeID(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

func FuzzSafeID(f *testing.F) {
	for _, seed := range []string{"session_01TEST", "CON", "cOm1", "COM¹", "../x", ""} {
		f.Add(seed)
	}
	allowed := regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	devices := regexp.MustCompile(`(?i)^(CON|PRN|AUX|NUL|COM[0-9]|LPT[0-9])$`)
	f.Fuzz(func(t *testing.T, id string) {
		if safeID(id) && (!allowed.MatchString(id) || devices.MatchString(id)) {
			t.Fatal("unsafe session ID accepted")
		}
	})
}
