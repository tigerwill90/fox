// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParams_Get(t *testing.T) {
	params := make(Params, 0, 2)
	params = append(params,
		Param{
			Key:   "foo",
			Value: "bar",
		},
		Param{
			Key:   "john",
			Value: "doe",
		},
	)
	assert.Equal(t, "bar", params.Get("foo"))
	assert.Equal(t, "doe", params.Get("john"))
}

func TestParams_Clone(t *testing.T) {
	params := make(Params, 0, 2)
	params = append(params,
		Param{
			Key:   "foo",
			Value: "bar",
		},
		Param{
			Key:   "john",
			Value: "doe",
		},
	)
	assert.Equal(t, params, params.Clone())
}

func TestParams_Has(t *testing.T) {
	t.Parallel()

	params := make(Params, 0, 2)
	params = append(params,
		Param{
			Key:   "foo",
			Value: "bar",
		},
		Param{
			Key:   "john",
			Value: "doe",
		},
	)

	assert.True(t, params.Has("foo"))
	assert.True(t, params.Has("john"))
	assert.False(t, params.Has("jane"))
}

func TestParamsFromContext(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		ctx            context.Context
		expectedParams Params
	}{
		{
			name:           "empty context",
			ctx:            context.Background(),
			expectedParams: nil,
		},
		{
			name: "context with params",
			ctx: func() context.Context {
				params := make(Params, 0, 2)
				params = append(params,
					Param{
						Key:   "foo",
						Value: "bar",
					},
				)
				return context.WithValue(context.Background(), paramsKey, params)
			}(),
			expectedParams: func() Params {
				params := make(Params, 0, 2)
				params = append(params,
					Param{
						Key:   "foo",
						Value: "bar",
					},
				)
				return params
			}(),
		},
	}

	for _, tc := range cases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			params := ParamsFromContext(tc.ctx)
			assert.Equal(t, tc.expectedParams, params)
		})
	}
}
