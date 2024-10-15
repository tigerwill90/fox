package fox

import (
	"cmp"
	"context"
	"iter"
	"log"
	"slices"
)

type hook struct {
	ctx context.Context
}

type ValidationTree interface {
	Has(method, path string) bool
	Route(method, path string) *Route
	Iter() Iter
}

type MutationTree interface {
	ValidationTree
	Handle(method, path string, handler HandlerFunc, opts ...PathOption) (*Route, error)
	Update(method, path string, handler HandlerFunc, opts ...PathOption) (*Route, error)
	Delete(method, path string, opts ...AdmissionOption) error
}

type ValidationRoute interface {
	Path() string
	Annotations() iter.Seq[Annotation]
	RedirectTrailingSlashEnabled() bool
	IgnoreTrailingSlashEnabled() bool
	ClientIPStrategyEnabled() bool
}

type MutationRoute interface {
	ValidationRoute
	SetPath(path string)
	PatchAnnotations(fn func(annotations iter.Seq[Annotation]) iter.Seq[Annotation])
	// TODO Patch middleware
	SetRedirectTrailingSlash(enable bool)
	SetIgnoreTrailingSlash(enable bool)
	SetClientIPStrategy(strategy ClientIPStrategy)
}

type AdmissionController interface {
	Handles(operation Operation) bool
	Admit(ctx context.Context, tree MutationTree, route MutationRoute) (err error)
	Validate(ctx context.Context, tree ValidationTree, route ValidationRoute) (err error)
}

type Operation uint8

const (
	Insert Operation = iota
	Update
	Delete
)

type mutationRoute struct {
	*Route
}

func (r mutationRoute) SetPath(path string) {
	r.path = path
}

func (r mutationRoute) PatchAnnotations(fn func(annotations iter.Seq[Annotation]) iter.Seq[Annotation]) {
	r.annots = slices.Collect(fn(slices.Values(r.annots)))
}

func (r mutationRoute) SetRedirectTrailingSlash(enable bool) {
	r.redirectTrailingSlash = enable
}

func (r mutationRoute) SetIgnoreTrailingSlash(enable bool) {
	r.ignoreTrailingSlash = enable
}

func (r mutationRoute) SetClientIPStrategy(strategy ClientIPStrategy) {
	r.ipStrategy = cmp.Or(strategy, ClientIPStrategy(noClientIPStrategy{}))
}

type validationRoute struct {
	*Route
}

func newController() *controller {
	c := &controller{
		c: make(chan ValidationRoute),
	}

	go func(c <-chan ValidationRoute) {
		for rte := range c {
			log.Println(rte.Path())
		}
	}(c.c)

	return c
}

type controller struct {
	c chan ValidationRoute
}

func (c *controller) Handles(operation Operation) bool {
	return operation == Insert || operation == Update
}

func (c *controller) Admit(ctx context.Context, tree MutationTree, route MutationRoute) (err error) {
	return nil
}

func (c *controller) Validate(ctx context.Context, tree ValidationTree, route ValidationRoute) (err error) {
	route.Annotations()
	return nil
}
