package fox

func newIterator(n *node) *iterator {
	return &iterator{
		stack: []stack{{edges: []*node{n}}},
	}
}

type iterator struct {
	current *node
	path    string
	stack   []stack
}

type stack struct {
	edges []*node
}

func (it *iterator) fullPath() string {
	return it.path
}

func (it *iterator) node() *node {
	return it.current
}

func (it *iterator) hasNext() bool {
	for len(it.stack) > 0 {
		n := len(it.stack)
		last := it.stack[n-1]
		elem := last.edges[0]

		if len(last.edges) > 1 {
			it.stack[n-1].edges = last.edges[1:]
		} else {
			it.stack = it.stack[:n-1]
		}

		if len(elem.children) > 0 {
			it.stack = append(it.stack, stack{elem.getEdgesShallowCopy()})
		}

		it.current = elem

		if it.current.isLeaf() {
			it.path = elem.path
			return true
		}
	}

	it.current = nil
	it.path = ""
	return false
}
