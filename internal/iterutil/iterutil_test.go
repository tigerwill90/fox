package iterutil

import (
	"github.com/stretchr/testify/assert"
	"slices"
	"testing"
)

func TestSplitStringSeq(t *testing.T) {
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
			got := slices.Collect(SplitStringSeq(tc.s, tc.sep))
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantLen, len(got))
		})
	}

	t.Run("break", func(t *testing.T) {
		k := 0
		parts := make([]string, 0, 1)
		for part := range SplitStringSeq("1,2,3", ",") {
			if k > 0 {
				break
			}
			parts = append(parts, part)
			k++
		}
		assert.Equal(t, []string{"1"}, parts)
	})
}

func TestSplitBytesSeq(t *testing.T) {
	cases := []struct {
		name    string
		s       []byte
		sep     []byte
		want    [][]byte
		wantLen int
	}{
		{
			name:    "split all empty",
			s:       []byte(""),
			sep:     []byte(","),
			want:    [][]byte{[]byte("")},
			wantLen: 1,
		},
		{
			name:    "split empty segment",
			s:       []byte("ab,cd,,,ef"),
			sep:     []byte(","),
			want:    [][]byte{[]byte("ab"), []byte("cd"), []byte(""), []byte(""), []byte("ef")},
			wantLen: 5,
		},
		{
			name:    "split with space string",
			s:       []byte(" "),
			sep:     []byte(","),
			want:    [][]byte{[]byte(" ")},
			wantLen: 1,
		},
		{
			name:    "split all",
			s:       []byte("ab,cd,ef"),
			sep:     []byte(","),
			want:    [][]byte{[]byte("ab"), []byte("cd"), []byte("ef")},
			wantLen: 3,
		},
		{
			name:    "split forwarded header",
			s:       []byte("by=<identifier>;for=<identifier>;host=<host>;proto=<http|https>"),
			sep:     []byte(";"),
			want:    [][]byte{[]byte("by=<identifier>"), []byte("for=<identifier>"), []byte("host=<host>"), []byte("proto=<http|https>")},
			wantLen: 4,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := slices.Collect(SplitBytesSeq(tc.s, tc.sep))
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantLen, len(got))
		})
	}

	t.Run("break", func(t *testing.T) {
		k := 0
		parts := make([][]byte, 0, 1)
		for part := range SplitBytesSeq([]byte("1,2,3"), []byte(",")) {
			if k > 0 {
				break
			}
			parts = append(parts, part)
			k++
		}
		assert.Equal(t, [][]byte{[]byte("1")}, parts)
	})
}

func TestBackwardSplitStringSeq(t *testing.T) {
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
			got := slices.Collect(BackwardSplitStringSeq(tc.s, tc.sep))
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantLen, len(got))
		})
	}

	t.Run("break", func(t *testing.T) {
		k := 0
		parts := make([]string, 0, 1)
		for part := range BackwardSplitStringSeq("1,2,3", ",") {
			if k > 0 {
				break
			}
			parts = append(parts, part)
			k++
		}
		assert.Equal(t, []string{"3"}, parts)
	})
}
