package internal

type Set[T comparable] struct {
	values map[T]struct{}
}

func NewSet[T comparable]() Set[T] {
	return Set[T]{
		values: make(map[T]struct{}),
	}
}

func (s *Set[T]) AddValues(values ...T) {
	for _, value := range values {
		s.values[value] = struct{}{}
	}
}

func (s *Set[T]) Contains(value T) bool {
	_, contains := s.values[value]
	return contains
}
