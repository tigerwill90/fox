package fox

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestRoute_HandleMiddlewareMalloc(t *testing.T) {
	f := New()
	for _, rte := range githubAPI {
		require.NoError(t, onlyError(f.Tree().Handle(rte.method, rte.path, emptyHandler)))
	}

	for _, rte := range githubAPI {
		req := httptest.NewRequest(rte.method, rte.path, nil)
		w := httptest.NewRecorder()
		r, c, _ := f.Lookup(&recorder{ResponseWriter: w}, req)
		allocs := testing.AllocsPerRun(100, func() {
			r.HandleMiddleware(c)
		})
		c.Close()
		assert.Equal(t, float64(0), allocs)
	}
}
