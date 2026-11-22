package reporter

import (
	"context"
	"net/url"
	"regexp"
	"strings"
)

// repositoryRemote reads fetch URLs and the recorded branch's tracking remote
// from local Git config, including a linked worktree's shared config.
func repositoryRemote(ctx context.Context, cwd, branch string) string {
	if cwd == "" {
		return ""
	}
	out, err := runGit(ctx, cwd, "config", "--local", "--no-includes", "--null", "--get-regexp", "^remote\\..*\\.url$|^branch\\..*\\.remote$")
	if err != nil {
		return ""
	}
	remotes := make(map[string][]string)
	var tracking string
	for _, entry := range splitNUL(out) {
		key, value, ok := strings.Cut(entry, "\n")
		if !ok {
			continue
		}
		if branch != "" && key == "branch."+branch+".remote" {
			tracking = value
		}
		if name, ok := strings.CutPrefix(key, "remote."); ok && strings.HasSuffix(name, ".url") {
			name = strings.TrimSuffix(name, ".url")
			remotes[name] = append(remotes[name], value)
		}
	}
	if tracking != "" {
		return singleRemote(remotes[tracking])
	}
	if len(remotes) == 1 {
		for _, urls := range remotes {
			return singleRemote(urls)
		}
	}
	return ""
}

func singleRemote(urls []string) string {
	if len(urls) != 1 {
		return ""
	}
	return remoteIdentity(urls[0])
}

var repoName = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*(:[0-9]{1,5})?/[a-z0-9._-]+/[a-z0-9._-]+$`)

// remoteIdentity accepts network fetch URLs and the usual SSH shorthand.
// Only a validated host and owner/repo can enter session metadata.
func remoteIdentity(raw string) string {
	var host, path string
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "ssh" && u.Scheme != "git") ||
			u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
			return ""
		}
		host = u.Host
		if (u.Scheme == "ssh" && u.Port() == "22") || (u.Scheme == "https" && u.Port() == "443") ||
			(u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "git" && u.Port() == "9418") {
			host = u.Hostname()
		}
		path = strings.TrimPrefix(u.Path, "/")
	} else {
		var ok bool
		host, path, ok = strings.Cut(raw, ":")
		if !ok || strings.ContainsAny(host, "/\\") || len(host) <= 1 {
			return ""
		}
		if _, hostname, ok := strings.Cut(host, "@"); ok {
			host = hostname
		}
	}
	path = strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(path), "/"), ".git")
	id := strings.ToLower(host) + "/" + path
	if !repoName.MatchString(id) || len(id) > 300 {
		return ""
	}
	for _, part := range strings.Split(path, "/") {
		if part == "." || part == ".." {
			return ""
		}
	}
	return id
}
