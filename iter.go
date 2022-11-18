package fox

func newIterator(n *node) *iterator {
	return &iterator{
		stack: []stack{{edges: []*node{n}}},
	}
}

type iterator struct {
	stack   []stack
	current *node
	path    string
}

type stack struct {
	path  string
	edges []*node
}

func (it *iterator) fullPath() string {
	return it.path
}

func (it *iterator) node() *node {
	return it.current
}

func (it *iterator) hasNextLeaf() bool {
	for it.hasNext() {
		if it.current.isLeaf() {
			return true
		}
	}
	return false
}

func (it *iterator) hasNext() bool {
	if len(it.stack) > 0 {
		n := len(it.stack)
		last := it.stack[n-1]
		elem := last.edges[0]

		if len(last.edges) > 1 {
			it.stack[n-1].edges = last.edges[1:]
		} else {
			it.stack = it.stack[:n-1]
		}

		if len(elem.children) > 0 {
			path := last.path + elem.key
			it.stack = append(it.stack, stack{path, elem.getEdgesShallowCopy()})
		}

		it.current = elem
		it.path = last.path + elem.key
		return true
	}

	it.current = nil
	it.path = ""
	return false
}
