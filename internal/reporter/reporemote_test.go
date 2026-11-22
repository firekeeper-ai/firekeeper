package reporter

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRemoteIdentity(t *testing.T) {
	for _, tc := range []struct{ url, want string }{
		{"git@github.com:Acme/Demo.git", "github.com/acme/demo"},
		{"https://user:example@github.com/Acme/Demo.git", "github.com/acme/demo"},
		{"ssh://git@github.com:22/Acme/Demo.git", "github.com/acme/demo"},
		{"https://code.example:8443/org/demo", "code.example:8443/org/demo"},
		{"https://github.com/acme/demo?private=value", ""},
		{"https://github.com/acme/../demo", ""},
		{"file:///home/user/demo", ""},
		{"C:/owner/repo", ""},
		{"/some/local/path", ""},
	} {
		if got := remoteIdentity(tc.url); got != tc.want {
			t.Errorf("remoteIdentity(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

func TestReportUsesWorktreeTrackingRemote(t *testing.T) {
	root, _ := sessionRepo(t)
	gitAt(t, root, gitT0, "remote", "add", "origin", "git@github.com:acme/demo.git")
	worktree := filepath.Join(t.TempDir(), "different-name")
	gitAt(t, root, gitT0, "worktree", "add", "-b", "topic", worktree)
	ctx := context.Background()
	if got := repositoryRemote(ctx, worktree, "topic"); got != "github.com/acme/demo" {
		t.Fatalf("worktree single remote = %q", got)
	}
	gitAt(t, root, gitT0, "remote", "add", "fork", "https://github.com/me/demo")
	if got := repositoryRemote(ctx, worktree, "topic"); got != "" {
		t.Fatalf("ambiguous remote = %q", got)
	}
	gitAt(t, root, gitT0, "config", "branch.topic.remote", "fork")
	if got := repositoryRemote(ctx, worktree, "topic"); got != "github.com/me/demo" {
		t.Fatalf("tracking remote = %q", got)
	}
}
