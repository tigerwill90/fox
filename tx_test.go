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

	fmt.Println("Before")
	_ = r.Walk(func(method, path string, handler Handler) error {
		fmt.Println(method, path)
		return nil
	})

	tx := NewTransaction(r, false)
	defer tx.Release()
	it := tx.NewIterator()
	for it.SeekMethod("DELETE"); it.Valid(); it.Next() {
		require.NoError(t, tx.Remove(it.Method(), it.Path()))
	}
	tx.Commit()

	fmt.Println("After")
	_ = r.Walk(func(method, path string, handler Handler) error {
		fmt.Println(method, path)
		return nil
	})
}
