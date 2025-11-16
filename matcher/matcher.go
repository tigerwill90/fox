package matcher

import "github.com/tigerwill90/fox"

func As[T fox.Matcher](matcher fox.Matcher, target *T) bool {
	if matcher == nil {
		return false
	}
	if target == nil {
		panic("fox: target cannot be nil")
	}
	return as(matcher, target)
}

func as[T fox.Matcher](matcher fox.Matcher, target *T) bool {
	for {
		if x, ok := matcher.(T); ok {
			*target = x
			return true
		}
		if x, ok := matcher.(interface{ As(any) bool }); ok && x.As(target) {
			return true
		}
		switch x := matcher.(type) {
		case interface{ Unwrap() fox.Matcher }:
			matcher = x.Unwrap()
			if matcher == nil {
				return false
			}
		case interface{ Unwrap() []fox.Matcher }:
			for _, matcher := range x.Unwrap() {
				if matcher == nil {
					continue
				}
				if as(matcher, target) {
					return true
				}
			}
			return false
		default:
			return false
		}
	}
}
