package fox

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestX(t *testing.T) {
	f, _ := New()

	require.NoError(t, f.Updates(func(txn *Txn) error {
		for _, rte := range githubAPI {
			if err := onlyError(txn.Handle(rte.method, rte.path, emptyHandler)); err != nil {
				return err
			}
		}
		for _, rte := range githubAPI {
			if err := onlyError(txn.Handle(rte.method, "{sub}.bar.{tld}"+rte.path, emptyHandler)); err != nil {
				return err
			}
		}

		fmt.Println(txn.Has(githubAPI[0].method, "{sub}.bar.{tld}"+githubAPI[0].path))
		return nil
	}))
	/*	tree := f.getTree()
		fmt.Println(tree.root[http.MethodGet])
		require.NoError(t, f.Updates(func(txn *Txn) error {
			for _, rte := range githubAPI {
				if err := onlyError(txn.Delete(rte.method, "{sub}.bar.{tld}"+rte.path)); err != nil {
					return err
				}
			}
			return nil
		}))

		tree = f.getTree()
		fmt.Println(tree.root[http.MethodGet])*/
}
