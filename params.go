// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

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

// Clone make a copy of Params.
func (p Params) Clone() Params {
	cloned := make(Params, len(p))
	copy(cloned, p)
	return cloned
}
