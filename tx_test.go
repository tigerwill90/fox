package fox

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestNewTransaction(t *testing.T) {
	r := New()
	for _, route := range githubAPI {
		require.NoError(t, r.Handler(route.method, route.path, HandlerFunc(func(w http.ResponseWriter, r *http.Request, params Params) {})))
	}

	fmt.Println("Before", r.maxParams)
	_ = r.Walk(func(method, path string, handler Handler) error {
		fmt.Println(method, path)
		return nil
	})

	tx := r.NewTransaction(true)
	defer tx.Discard()
	tx.Commit()

	fmt.Println("After", r.maxParams)
	_ = r.Walk(func(method, path string, handler Handler) error {
		fmt.Println(method, path)
		return nil
	})
}
