package fox

type Tx struct {
	r   *LockedRouter
	tmp *LockedRouter
}

func NewTransaction(fox *Router, reset bool) *Tx {
	r := fox.LockRouter()
	tmp := New().LockRouter()
	if reset {
		return &Tx{
			r:   r,
			tmp: tmp,
		}
	}

	it := r.NewIterator()
	for it.Rewind(); it.Valid(); it.Next() {
		_ = tmp.Handler(it.Method(), it.Path(), it.Handler())
	}

	return &Tx{
		r:   r,
		tmp: tmp,
	}
}

func (tx *Tx) Handler(method, path string, handler Handler) error {
	return tx.tmp.Handler(method, path, handler)
}

func (tx *Tx) Update(method, path string, handler Handler) error {
	return tx.tmp.Update(method, path, handler)
}

func (tx *Tx) Remove(method, path string) error {
	return tx.tmp.Remove(method, path)
}

func (tx *Tx) NewIterator() *Iterator {
	return tx.tmp.NewIterator()
}

func (tx *Tx) Commit() {
	tx.r.assertLock()
	tx.tmp.assertLock()

	nds := tx.tmp.r.trees.Load()
	tx.r.r.trees.Store(nds)
}

func (tx *Tx) Release() {
	tx.tmp.Release()
	tx.r.Release()
}
