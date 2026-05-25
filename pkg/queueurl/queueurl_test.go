package queueurl

import "testing"

func TestIsPlaybackURL(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"https://cdn.example/v.mp4", true},
		{"http://x", true},
		{"//cdn.example/v.m3u8", true},
		{"video-1", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := IsPlaybackURL(tc.in); got != tc.want {
			t.Fatalf("IsPlaybackURL(%q) = %v want %v", tc.in, got, tc.want)
		}
	}
}
