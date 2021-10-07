package RingBuffer

import (
	"errors"
	"fmt"
)

type RB struct {
	len int
	buf []interface{}
	idx []int
	exe int
}

func (rb *RB) Arrayindex(index int) int {
	return index % rb.len
}

func (rb *RB) Set(index int, val interface{}) error {
	//empty cell
	idx := rb.Arrayindex(index)
	if rb.idx[idx] < 0 && index == idx {
		rb.idx[idx] = index
		rb.buf[idx] = val
		return nil
	}
	//err wrong index
	if index-rb.idx[idx] != rb.len {
		return errors.New("set wrong index")
	}

	// next one is up-to-date
	if index-rb.idx[rb.Arrayindex(index+1)] == rb.len-1 {
		rb.idx[idx] = index
		rb.buf[idx] = val
		return nil
	}
	// didn't update for a long time.
	return errors.New("set wrong index")
}

func (rb *RB) Get(index int) (interface{}, error) {
	if index > -1 {
		idx := rb.Arrayindex(index)
		if rb.idx[idx] == index {
			return rb.buf[idx], nil
		}
		msg := fmt.Sprintf("index:%v != stored index %v", index, rb.idx[idx])
		return nil, errors.New(msg)
	}
	msg := fmt.Sprintf("index:%v < 0", index)
	return nil, errors.New(msg)
}

func NewRB(len int) *RB {
	rb := RB{len: len}
	rb.len = len
	rb.buf = make([]interface{}, len)
	rb.idx = make([]int, len)
	rb.exe = 0
	for i := 0; i < len; i++ {
		rb.buf[i] = nil
		rb.idx[i] = -1
	}
	return &rb
}
