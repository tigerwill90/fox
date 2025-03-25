package signals

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestSetupHandlerOnce(t *testing.T) {
	SetupHandler()
	assert.Panics(t, func() {
		SetupHandler()
	})
}
