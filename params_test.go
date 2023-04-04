// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"github.com/stretchr/testify/assert"
	"testing"
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
