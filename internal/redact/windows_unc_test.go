package redact

import (
	"strings"
	"testing"
)

func TestUNCHomeSpellings(t *testing.T) {
	saved := shortPathName
	t.Cleanup(func() { shortPathName = saved })
	shortPathName = func(string) string {
		t.Fatal("UNC redaction must not resolve a network path")
		return ""
	}
	for _, home := range []string{`\\server\profiles\Dan`, `//server/profiles/Dan`, `\\?\UNC\server\profiles\Dan`} {
		opts := Options{HomeDir: home}
		for _, tt := range []struct{ in, want string }{
			{`\\SERVER\PROFILES\dan\src`, `~\src`},
			{`//SERVER/profiles/dan/src`, `~/src`},
			{`\\server/profiles\dan`, `~`},
			{`\\\\server\\profiles\\Dan\\src`, `~\\src`},
			{`\\?\UNC\SERVER\profiles\dan\src`, `~\src`},
			{`file://server/profiles/Dan/src`, `file:~/src`},
			{`%5C%5C%73erver%5Cprofiles%5CD%61n%5Csrc`, `~%5Csrc`},
			{`//server/profiles/Dan.`, `~.`},
			{`//server/profiles/Dan.old/src`, `//server/profiles/Dan.old/src`},
			{`//server/profiles/Dan%6Fld`, `//server/profiles/Dan%6Fld`},
			{`//other/profiles/Dan`, `//other/profiles/Dan`},
			{`//server/other/Dan`, `//server/other/Dan`},
			{`/server/profiles/Dan`, `/server/profiles/Dan`},
			{`x//server/profiles/Dan`, `x//server/profiles/Dan`},
		} {
			t.Run(home+" "+tt.in, func(t *testing.T) {
				counts := Counts{}
				if tt.in != tt.want {
					counts[KindHomeDir] = 1
				}
				assertPathRedaction(t, tt.in, tt.want, opts, counts)
			})
		}
	}
}

func TestUNCConfiguredPathTildeForms(t *testing.T) {
	opts := Options{HomeDir: `\\server\profiles\Dan`, Paths: []string{`\\?\UNC\SERVER\profiles\dan\Private`}}
	assertPathRedaction(t, `~/private/file ~\PRIVATE\file //server/profiles/Dan/private/file`,
		`[REDACTED:path]/file [REDACTED:path]\file [REDACTED:path]/file`, opts, Counts{KindPath: 3})
	assertPathRedaction(t, `//server/profiles/Dan/privately/file`,
		`~/privately/file`, opts, Counts{KindHomeDir: 1})
	if _, ok := homeForm(`\\other\profiles\Dan\Private`, opts.HomeDir); ok {
		t.Fatal("derived a home form for a different server")
	}
	if _, ok := homeForm(`\\server\profiles\Dan.old\Private`, opts.HomeDir); ok {
		t.Fatal("derived a home form for a longer username")
	}
}

func FuzzWindowsUNCPaths(f *testing.F) {
	for _, p := range []string{`\\server\profiles\Dan`, `//server/profiles/Dan`, `\\?\UNC\server\profiles\Dan`, `\\.\pipe\name`, `\\server`, `\\server\\share`, "//server/share/\xff"} {
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, p string) {
		normalized, ok := windowsUNCPath(p)
		if !ok {
			return
		}
		if !strings.HasPrefix(normalized, "//") {
			t.Fatal("UNC path lost its root")
		}
		if again, ok := windowsUNCPath(normalized); !ok || again != normalized {
			t.Fatal("UNC normalization is not idempotent")
		}
		w := compileWindowsPath(p, false)
		if w == nil || !w.re.MatchString(normalized) {
			t.Fatal("valid UNC path has no matching pattern")
		}
	})
}
