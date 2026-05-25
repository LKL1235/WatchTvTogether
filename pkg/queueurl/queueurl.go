package queueurl

import "strings"

// IsPlaybackURL reports whether s is an absolute playback URL for room queues.
func IsPlaybackURL(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "//") {
		return len(s) > 2
	}
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// FilterPlaybackURLs returns only valid playback URLs, in order.
func FilterPlaybackURLs(queue []string) []string {
	out := make([]string, 0, len(queue))
	for _, item := range queue {
		if IsPlaybackURL(item) {
			out = append(out, strings.TrimSpace(item))
		}
	}
	return out
}
