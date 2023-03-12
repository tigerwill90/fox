package fox

import (
	"github.com/stretchr/testify/assert"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWrapHandler(t *testing.T) {
	tree := New().Tree()

	cases := []struct {
		name   string
		h      Handler
		params Params
	}{
		{
			name: "wrapf with params",
			h: WrapF(func(w http.ResponseWriter, r *http.Request) {
				params := ParamsFromContext(r.Context())
				assert.Equal(t, "bar", params.Get("foo"))
				assert.Equal(t, "doe", params.Get("john"))
			}),
			params: Params{
				Param{
					Key:   "foo",
					Value: "bar",
				},
				Param{
					Key:   "john",
					Value: "doe",
				},
			},
		},
		{
			name: "wraph with params",
			h: WrapH(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				params := ParamsFromContext(r.Context())
				assert.Equal(t, "bar", params.Get("foo"))
				assert.Equal(t, "doe", params.Get("john"))
			})),
			params: Params{
				Param{
					Key:   "foo",
					Value: "bar",
				},
				Param{
					Key:   "john",
					Value: "doe",
				},
			},
		},
		{
			name: "wrapf no params",
			h: WrapF(func(w http.ResponseWriter, r *http.Request) {
				params := ParamsFromContext(r.Context())
				assert.Nil(t, params)
			}),
			params: nil,
		},
		{
			name: "wraph no params",
			h: WrapH(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				params := ParamsFromContext(r.Context())
				assert.Nil(t, params)
			})),
			params: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := tree.newParams()
			defer params.Free(tree)
			*params = append(*params, tc.params...)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			tc.h.ServeHTTP(nil, req, *params)
		})
	}
}

func TestParamsClone(t *testing.T) {
	tree := New().Tree()
	params := tree.newParams()
	defer params.Free(tree)
	*params = append(*params,
		Param{
			Key:   "foo",
			Value: "bar",
		},
		Param{
			Key:   "john",
			Value: "doe",
		},
	)
	assert.Equal(t, *params, params.Clone())
}
