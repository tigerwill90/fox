package bytesconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBytes(t *testing.T) {
	assert.Equal(t, Bytes("foo"), []byte("foo"))
}

func TestString(t *testing.T) {
	assert.Equal(t, String([]byte("foo")), "foo")
}
