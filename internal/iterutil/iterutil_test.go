package iterutil

import (
	"github.com/stretchr/testify/assert"
	"slices"
	"testing"
)

func TestSplitSeqN(t *testing.T) {
	cases := []struct {
		name    string
		s       string
		n       uint
		sep     string
		want    []string
		wantLen int
	}{
		{
			name:    "split 0",
			s:       "ab,cd,ef",
			n:       0,
			sep:     ",",
			want:    []string(nil),
			wantLen: 0,
		},
		{
			name:    "split empty segment",
			s:       "ab,cd,,,ef",
			n:       5,
			sep:     ",",
			want:    []string{"ab", "cd", "", "", "ef"},
			wantLen: 5,
		},
		{
			name:    "split 1",
			s:       "ab,cd,ef",
			n:       1,
			sep:     ",",
			want:    []string{"ab,cd,ef"},
			wantLen: 1,
		},
		{
			name:    "split 2",
			s:       "ab,cd,ef",
			n:       2,
			sep:     ",",
			want:    []string{"ab", "cd,ef"},
			wantLen: 2,
		},
		{
			name:    "split with empty string",
			s:       "",
			n:       2,
			sep:     ",",
			want:    []string{""},
			wantLen: 1,
		},
		{
			name:    "split all",
			s:       "ab,cd,ef",
			n:       3,
			sep:     ",",
			want:    []string{"ab", "cd", "ef"},
			wantLen: 3,
		},
		{
			name:    "split more",
			s:       "ab,cd,ef",
			n:       4,
			sep:     ",",
			want:    []string{"ab", "cd", "ef"},
			wantLen: 3,
		},
		{
			name:    "split even more",
			s:       "ab,cd,ef",
			n:       20,
			sep:     ",",
			want:    []string{"ab", "cd", "ef"},
			wantLen: 3,
		},
		{
			name:    "explode 0",
			s:       "abcdef",
			n:       0,
			sep:     "",
			want:    []string(nil),
			wantLen: 0,
		},
		{
			name:    "explode 1",
			s:       "abcdef",
			n:       1,
			sep:     "",
			want:    []string{"abcdef"},
			wantLen: 1,
		},
		{
			name:    "explode 2",
			s:       "abcdef",
			n:       2,
			sep:     "",
			want:    []string{"a", "bcdef"},
			wantLen: 2,
		},
		{
			name:    "explode 3",
			s:       "abcdef",
			n:       3,
			sep:     "",
			want:    []string{"a", "b", "cdef"},
			wantLen: 3,
		},
		{
			name:    "explode with empty string",
			s:       "",
			n:       3,
			sep:     "",
			want:    []string(nil),
			wantLen: 0,
		},
		{
			name:    "explode all",
			s:       "abcdef",
			n:       6,
			sep:     "",
			want:    []string{"a", "b", "c", "d", "e", "f"},
			wantLen: 6,
		},
		{
			name:    "explode more",
			s:       "abcdef",
			n:       7,
			sep:     "",
			want:    []string{"a", "b", "c", "d", "e", "f"},
			wantLen: 6,
		},
		{
			name:    "explode even more",
			s:       "abcdef",
			n:       20,
			sep:     "",
			want:    []string{"a", "b", "c", "d", "e", "f"},
			wantLen: 6,
		},
		{
			name:    "split forwarded header",
			s:       "by=<identifier>;for=<identifier>;host=<host>;proto=<http|https>",
			n:       4,
			sep:     ";",
			want:    []string{"by=<identifier>", "for=<identifier>", "host=<host>", "proto=<http|https>"},
			wantLen: 4,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := slices.Collect(SplitSeqN(tc.s, tc.sep, tc.n))
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantLen, len(got))
		})
	}
}
