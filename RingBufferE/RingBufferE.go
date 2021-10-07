/*
determin by execute
*/

package RingBufferE

import (
	"errors"
	"fmt"
	"sync"
)

type RBE struct {
	len       int // sizeof buff
	next      int // next to execut
	maxcommit int
	wr        int // total rounds to wait before say I am dead
	buf       []interface{}
	// toexe  []int // times try to execute, init as 1 for each new entry
	commit []bool
	idx    []int
	mu     sync.Mutex
}

func (rb *RBE) Arrayindex(index int) int {
	return index % rb.len
}

func (rb *RBE) Set(index int, val interface{}) error {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if index < 0 {
		msg := fmt.Sprintf("index:%v < 0", index)
		return errors.New(msg)
	}
	idx := rb.Arrayindex(index)
	// Nothing is waiting to be executed
	if index-rb.next < rb.len {
		// correct index || init setup
		if index == rb.idx[idx]+rb.len || rb.idx[idx] < 0 {
			rb.buf[idx] = val
			rb.idx[idx] = index
			rb.commit[idx] = false
			return nil
		}
		// Index messed up
		return errors.New("Index messed up")
	}
	return errors.New("there's something hasn't been executed yet")
}

func (rb *RBE) CommitIndex(index int) error {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if index < 0 {
		msg := fmt.Sprintf("index:%v < 0", index)
		return errors.New(msg)
	}
	idx := rb.Arrayindex(index)
	// Index messed up
	if rb.idx[idx] != index {
		msg := fmt.Sprintf("index:%v != stored index %v", index, rb.idx[idx])
		return errors.New(msg)
	}
	rb.commit[idx] = true
	if index > rb.maxcommit {
		rb.maxcommit = index
	}
	return nil
}

func (rb *RBE) Get(index int) (interface{}, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if index < 0 {
		msg := fmt.Sprintf("index:%v < 0", index)
		return nil, errors.New(msg)
	}
	idx := rb.Arrayindex(index)
	// Index messed up
	if rb.idx[idx] != index {
		msg := fmt.Sprintf("index:%v != stored index %v", index, rb.idx[idx])
		return nil, errors.New(msg)
	}
	return rb.buf[idx], nil
}

// First thing returned is nil unless there's something ready to be executed
func (rb *RBE) NextToExe() (interface{}, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	idx := rb.Arrayindex(rb.next)
	// There's something ready to be executed!
	if rb.maxcommit < rb.next {
		return nil, nil
	}
	if rb.commit[idx] {
		rb.next++
		return rb.buf[idx], nil
	}
	if rb.maxcommit-rb.next > rb.wr {
		msg := fmt.Sprintf("big gap between to execute(%v) and maxcommit(%v)", rb.next, rb.maxcommit)
		return nil, errors.New(msg)
	}
	// Nothing is ready to be executed now. return nil
	return nil, nil
}

func NewRBE(len int, wr int) *RBE {
	rb := RBE{len: len}
	rb.len = len
	rb.next = 0
	rb.buf = make([]interface{}, len)
	rb.maxcommit = 0
	rb.commit = make([]bool, len)
	rb.idx = make([]int, len)
	rb.wr = wr
	for i := 0; i < len; i++ {
		rb.buf[i] = nil
		rb.commit[i] = false
		rb.idx[i] = -1
	}
	return &rb
}
