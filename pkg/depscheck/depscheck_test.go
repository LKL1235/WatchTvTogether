package depscheck

import (
	"strconv"
	"testing"
)

func TestFFmpegVersionRegex(t *testing.T) {
	cases := []struct {
		line       string
		wantMajor  int
		wantSubnum int
	}{
		{"ffmpeg version 6.1.1-3ubuntu5 Copyright (c) 2000-2023 the FFmpeg developers", 6, 1},
		{"ffmpeg version 7.0", 7, 0},
		{"ffmpeg version 5.0", 5, 0},
	}
	for _, tc := range cases {
		m := ffmpegVersionLine.FindStringSubmatch(tc.line)
		if m == nil {
			t.Fatalf("no match: %q", tc.line)
		}
		major, _ := strconv.Atoi(m[1])
		if major != tc.wantMajor {
			t.Errorf("%q: major = %d, want %d", tc.line, major, tc.wantMajor)
		}
		if len(m) > 2 && m[2] != "" {
			minor, _ := strconv.Atoi(m[2])
			if minor != tc.wantSubnum {
				t.Errorf("%q: minor = %d, want %d", tc.line, minor, tc.wantSubnum)
			}
		}
	}
}
