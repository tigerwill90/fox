package iterutil

import (
	"github.com/stretchr/testify/assert"
	"slices"
	"testing"
)

func TestSplitSeq(t *testing.T) {
	cases := []struct {
		name    string
		s       string
		sep     string
		want    []string
		wantLen int
	}{
		{
			name:    "split all empty",
			s:       "",
			sep:     ",",
			want:    []string{""},
			wantLen: 1,
		},
		{
			name:    "split empty segment",
			s:       "ab,cd,,,ef",
			sep:     ",",
			want:    []string{"ab", "cd", "", "", "ef"},
			wantLen: 5,
		},
		{
			name:    "split with space string",
			s:       " ",
			sep:     ",",
			want:    []string{" "},
			wantLen: 1,
		},
		{
			name:    "split all",
			s:       "ab,cd,ef",
			sep:     ",",
			want:    []string{"ab", "cd", "ef"},
			wantLen: 3,
		},
		{
			name:    "split forwarded header",
			s:       "by=<identifier>;for=<identifier>;host=<host>;proto=<http|https>",
			sep:     ";",
			want:    []string{"by=<identifier>", "for=<identifier>", "host=<host>", "proto=<http|https>"},
			wantLen: 4,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := slices.Collect(SplitSeq(tc.s, tc.sep))
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantLen, len(got))
		})
	}

	t.Run("panic on empty sep", func(t *testing.T) {
		assert.Panics(t, func() {
			SplitSeq("a,b,c", "")
		})
	})
}

func TestBackwardSplitSeq(t *testing.T) {
	cases := []struct {
		name    string
		s       string
		sep     string
		want    []string
		wantLen int
	}{
		{
			name:    "split all empty",
			s:       "",
			sep:     ",",
			want:    []string{""},
			wantLen: 1,
		},
		{
			name:    "split empty segment",
			s:       "ab,cd,,,ef",
			sep:     ",",
			want:    []string{"ef", "", "", "cd", "ab"},
			wantLen: 5,
		},
		{
			name:    "split with space string",
			s:       " ",
			sep:     ",",
			want:    []string{" "},
			wantLen: 1,
		},
		{
			name:    "split all",
			s:       "ab,cd,ef",
			sep:     ",",
			want:    []string{"ef", "cd", "ab"},
			wantLen: 3,
		},
		{
			name:    "split forwarded header",
			s:       "by=<identifier>;for=<identifier>;host=<host>;proto=<http|https>",
			sep:     ";",
			want:    []string{"proto=<http|https>", "host=<host>", "for=<identifier>", "by=<identifier>"},
			wantLen: 4,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := slices.Collect(BackwardSplitSeq(tc.s, tc.sep))
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantLen, len(got))
		})
	}

	t.Run("panic on empty sep", func(t *testing.T) {
		assert.Panics(t, func() {
			BackwardSplitSeq("a,b,c", "")
		})
	})
}
