package internal

type Set[T comparable] struct {
	values map[T]struct{}
}

func NewSetWithValues[T comparable](values ...T) Set[T] {
	state := make(map[T]struct{}, len(values))
	for _, value := range values {
		state[value] = struct{}{}
	}
	return Set[T]{
		values: state,
	}
}

func (s *Set[T]) Contains(value T) bool {
	_, contains := s.values[value]
	return contains
}
