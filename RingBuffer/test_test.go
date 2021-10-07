package RingBuffer

import (
	// "crypto/rand"
	"fmt"
	"math/rand"
	"testing"
	"time"
)

var len int = 99999
var counter = 0
var maxgap = 10

type LogEntry struct {
	Term    int
	Command interface{}
}

func Test_Correct(t *testing.T) {
	fmt.Println("\ncorrect test started...")
	defer fmt.Println("finished...")
	rb := NewRB(len)
	counter = 0
	for counter < len*10 {
		err := rb.Set(counter, LogEntry{counter, counter})
		if err != nil {
			t.Fatalf("set faild at index %v\n", counter)
		}
		counter++
	}
	for i := len * 9; i < len*10; i++ {
		entry, err := rb.Get(i)
		if err != nil {
			t.Fatalf("get faild at index %v\n %v\n", i, err)
		}
		en := entry.(LogEntry)
		if en.Term%len != i%len {
			t.Fatalf("misorder at index %v\n", i)
		}
	}
}

func Test_lagged1(t *testing.T) {
	fmt.Println("\nlagged1 test started...")
	defer fmt.Println("finished...")
	counter = 0
	rb := NewRB(len)
	for counter < len*10 {
		err := rb.Set(counter, LogEntry{counter, counter})
		if err != nil {
			t.Fatalf("set faild at index %v\n", counter)
		}
		counter++
	}
	rb.idx[1] = 10
	err := rb.Set(0, LogEntry{0, 0})
	if err == nil {
		t.Fatalf("Should faild at index %v\n", 0)
	}
}

func Test_lagged2(t *testing.T) {
	fmt.Println("\nlagged2 test started...")
	defer fmt.Println("finished...")
	for i := 0; i < 100; i++ {
		counter = 0
		rb := NewRB(len)
		for counter < len {
			err := rb.Set(counter, LogEntry{counter, counter})
			if err != nil {
				t.Fatalf("set faild at index %v\n", counter)
			}
			counter++
		}
		s := rand.NewSource(time.Now().UnixNano())
		r := rand.New(s)
		rl := r.Intn(len) + len
		for counter < len*2 {
			if counter == rl {
				counter++
				continue
			}
			err := rb.Set(counter, LogEntry{counter, counter})
			if err != nil {
				t.Fatalf("set faild at index %v\n", counter)
			}
			counter++
		}
		err := rb.Set(rl+len, LogEntry{rl, rl})
		if err == nil {
			t.Fatalf("Should faild at index %v\n", rl)
		}
		err = rb.Set(rl+len-1, LogEntry{rl - 1, rl - 1})
		if err == nil {
			t.Fatalf("Should faild at index %v\n", rl)
		}

	}
}

func Test_go_back(t *testing.T) {
	fmt.Println("\ngo_back test started...")
	defer fmt.Println("finished...")
	counter = 0
	rb := NewRB(len)
	for counter < len*10 {
		err := rb.Set(counter, LogEntry{counter, counter})
		if err != nil {
			t.Fatalf("set faild at index %v\n", counter)
		}
		counter++
	}
	counter = 0
	for counter < len*9 {
		err := rb.Set(counter, LogEntry{counter, counter})
		if err == nil {
			t.Fatalf("Should faild at index %v\n", counter)
		}
		counter++
	}
}
func Test_Empty_get(t *testing.T) {
	fmt.Println("\nempty test started...")
	defer fmt.Println("finished...")
	counter = 0
	rb := NewRB(len)
	for i := 0; i < len; i++ {
		_, err := rb.Get(i)
		// en := entry.(LogEntry)
		if err == nil {
			t.Fatalf("Should faild at index %v\n", i)
		}
	}
}
func Test_go_race(t *testing.T) {
	fmt.Println("\nrace test started...")
	defer fmt.Println("finished...")
	rb := NewRB(len)
	go func(t *testing.T) {
		for i := 0; i < len/4; i++ {
			err := rb.Set(i*4, LogEntry{i * 4, i * 4})
			// en := entry.(LogEntry)
			if err != nil {
				t.Fatalf("Should faild at index %v\n", i)
			}
		}
	}(t)
	go func(t *testing.T) {
		for i := 0; i < len/4; i++ {
			err := rb.Set(i*4+1, LogEntry{i*4 + 1, i*4 + 1})
			// en := entry.(LogEntry)
			if err != nil {
				t.Fatalf("Should faild at index %v\n", i)
			}
		}
	}(t)
	go func(t *testing.T) {
		for i := 0; i < len/4; i++ {
			err := rb.Set(i*4+2, LogEntry{i*4 + 2, i*4 + 2})
			// en := entry.(LogEntry)
			if err != nil {
				t.Fatalf("Should faild at index %v\n", i)
			}
		}
	}(t)
	go func(t *testing.T) {
		for i := 0; i < len/4; i++ {
			err := rb.Set(i*4+3, LogEntry{i*4 + 3, i*4 + 3})
			// en := entry.(LogEntry)
			if err != nil {
				t.Fatalf("Should faild at index %v\n", i)
			}
		}
	}(t)
}
