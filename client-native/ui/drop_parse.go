package ui

import (
	"net/url"
	"path/filepath"
	"strings"
)

// parseDroppedURIList parses text/uri-list (and simple text/plain paths) into local file paths.
func parseDroppedURIList(raw string) []string {
	paths := make([]string, 0, 8)
	seen := make(map[string]struct{})
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		path := parseDroppedPath(line)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	return paths
}

func parseDroppedPath(line string) string {
	// Standard URI form: file:///path or file://localhost/path.
	if strings.HasPrefix(line, "file:") {
		u, err := url.Parse(line)
		if err != nil || u.Scheme != "file" {
			return ""
		}
		host := strings.TrimSpace(u.Host)
		if host != "" && host != "localhost" {
			// Ignore non-local hosts.
			return ""
		}
		p := u.EscapedPath()
		if p == "" {
			p = u.Path
		}
		decoded, err := url.PathUnescape(p)
		if err != nil || decoded == "" {
			return ""
		}
		return decoded
	}

	// Fallback for plain absolute paths.
	if filepath.IsAbs(line) {
		return line
	}
	return ""
}
