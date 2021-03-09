package lib

import (
	"testing"
)

func Test(t *testing.T) {
	s := new(Stack)

	if s.Len() != 0 {
		t.Errorf("Length of an empty stack should be 0")
	}

	s.Push(1)

	if s.Len() != 1 {
		t.Errorf("Length should be 0")
	}

	if s.Peek().(int) != 1 {
		t.Errorf("item on the stack should be 1")
	}

	if s.Pop().(int) != 1 {
		t.Errorf("item should have been 1")
	}

	if s.Len() != 0 {
		t.Errorf("Stack should be empty")
	}

	s.Push(1)
	s.Push(2)

	if s.Len() != 2 {
		t.Errorf("Length should be 2")
	}

	if s.Peek().(int) != 2 {
		t.Errorf("of the stack should be 2")
	}
}
