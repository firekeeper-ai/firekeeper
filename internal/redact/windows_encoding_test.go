package redact

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestWindowsMixedPercentEncoding(t *testing.T) {
	opts := Options{HomeDir: `C:\Users\Dan`}
	for _, in := range []string{
		percentEncode(opts.HomeDir),
		strings.ToLower(percentEncode(opts.HomeDir)),
		percentEncode(`c:\users\dan`),
		`C%3A%5C%55sers%5CD%61n`,
		`%63:/U%73ers/%64a%6E`,
		`file:///%43%3A/%55sers/%44an`,
	} {
		t.Run(in, func(t *testing.T) {
			prefix := ""
			if strings.HasPrefix(in, "file:///") {
				prefix = "file:///"
			}
			assertPathRedaction(t, in+"%5Csrc", prefix+"~%5Csrc", opts, Counts{KindHomeDir: 1})
		})
	}
	for _, suffix := range []string{"%6Fld", "%2Eold", "%C3%A9", "%", "%ZZ", "%2e%2Fchild"} {
		in := percentEncode(opts.HomeDir) + suffix
		assertPathRedaction(t, in, in, opts, Counts{})
	}
	for _, prefix := range []string{"x", ".", "%78", "%C3%A9"} {
		in := prefix + percentEncode(opts.HomeDir)
		assertPathRedaction(t, in, in, opts, Counts{})
	}
	for _, prefix := range []string{"%20", "%22", "?cwd="} {
		assertPathRedaction(t, prefix+percentEncode(opts.HomeDir), prefix+"~", opts, Counts{KindHomeDir: 1})
	}
	for _, suffix := range []string{"%2e", "%2E%20Next", ".%22", "%2Fchild"} {
		assertPathRedaction(t, percentEncode(opts.HomeDir)+suffix, "~"+suffix, opts, Counts{KindHomeDir: 1})
	}
	for _, in := range []string{percentEncode(`D:\Users\Dan`), `%43%3A%5C%ZZsers%5CDan`} {
		assertPathRedaction(t, in, in, opts, Counts{})
	}
}

func TestWindowsEncodedConfiguredPaths(t *testing.T) {
	opts := Options{HomeDir: `C:\Users\Dan`, Paths: []string{`C:\Users\Dan\private`}}
	in := percentEncode(opts.Paths[0]) + "%2Ffile ~/%70rivate/file"
	assertPathRedaction(t, in, "[REDACTED:path]%2Ffile [REDACTED:path]/file", opts, Counts{KindPath: 2})
	opts = Options{HomeDir: `C:\Users\J` + "\u00fcrgen"}
	in = percentEncode(`c:\users\J` + "\u00dcRGEN")
	assertPathRedaction(t, in, "~", opts, Counts{KindHomeDir: 1})
}

func FuzzWindowsPercentEncodedPaths(f *testing.F) {
	f.Add(`C:\Users\Dan`, uint64(0))
	f.Add(`c:\users\dan`, ^uint64(0))
	f.Add(`C:\Users\Dan%6Fld`, uint64(42))
	f.Fuzz(func(t *testing.T, text string, mask uint64) {
		if !utf8.ValidString(text) {
			return
		}
		opts := Options{HomeDir: `C:\Users\Dan`}
		var encoded strings.Builder
		for i := range len(opts.HomeDir) {
			c := opts.HomeDir[i : i+1]
			if mask&(1<<i) != 0 {
				c = percentEncode(c)
			}
			encoded.WriteString(c)
		}
		assertPathRedaction(t, encoded.String()+"%2Fsrc", "~%2Fsrc", opts, Counts{KindHomeDir: 1})
		got, counts := String(text, opts)
		assertPathRedaction(t, text, got, opts, counts)
	})
}
