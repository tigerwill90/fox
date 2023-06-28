// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import netcontext "context"

type ctxKey struct{}

// paramsKey is the key that holds the Params in a context.Context.
var paramsKey = ctxKey{}

type Param struct {
	Key   string
	Value string
}

type Params []Param

// Get the matching wildcard segment by name.
func (p Params) Get(name string) string {
	for i := range p {
		if p[i].Key == name {
			return p[i].Value
		}
	}
	return ""
}

// Has checks whether the parameter exists by name.
func (p Params) Has(name string) bool {
	for i := range p {
		if p[i].Key == name {
			return true
		}
	}

	return false
}

// Clone make a copy of Params.
func (p Params) Clone() Params {
	cloned := make(Params, len(p))
	copy(cloned, p)
	return cloned
}

// ParamsFromContext is a helper to retrieve params from context when a http.Handler
// is registered using WrapF or WrapH.
func ParamsFromContext(ctx netcontext.Context) Params {
	p, _ := ctx.Value(paramsKey).(Params)
	return p
}
