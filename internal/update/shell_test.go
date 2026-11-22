package update

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWindowsShellPrefersSystemPowerShell(t *testing.T) {
	sys := filepath.Join("C:", "Windows", "System32")
	system := filepath.Join(sys, "WindowsPowerShell", "v1.0", "powershell.exe")
	planted := func(name string) (string, error) { return filepath.Join("planted", name+".exe"), nil }
	missing := func(string) (string, error) { return "", exec.ErrNotFound }
	only := func(want string) func(string) bool { return func(p string) bool { return p == want } }
	none := func(string) bool { return false }

	tests := []struct {
		name    string
		sysDir  string
		exists  func(string) bool
		look    func(string) (string, error)
		want    string
		wantErr bool
	}{
		{"system powershell wins over PATH", sys, only(system), planted, system, false},
		{"pwsh on PATH when system copy is missing", sys, none, planted, filepath.Join("planted", "pwsh.exe"), false},
		{"no system directory falls back to pwsh", "", only(system), planted, filepath.Join("planted", "pwsh.exe"), false},
		{"nothing found", sys, none, missing, "", true},
		{"empty lookup result", "", none, func(string) (string, error) { return "", nil }, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var asked []string
			look := func(name string) (string, error) { asked = append(asked, name); return tt.look(name) }
			got, err := windowsShell(tt.sysDir, tt.exists, look)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("windowsShell = %q, %v; want %q, error %v", got, err, tt.want, tt.wantErr)
			}
			for _, name := range asked {
				if name == "powershell" {
					t.Fatal("searched PATH for powershell")
				}
			}
		})
	}
}
