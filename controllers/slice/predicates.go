package slice

func EqualsTo[T comparable](x T) func(T) bool {
	return func(y T) bool {
		return x == y
	}
}
