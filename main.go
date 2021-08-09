package main

import (
	"fmt"
	"sync"
	"time"
)

type test struct {
	mu    sync.Mutex // guards fields below it
	revMu sync.RWMutex
	mapp  map[string]string
}

func main() {
	test := test{
		mapp: map[string]string{
			"test1": "1",
			"test2": "2",
			"test3": "3",
			"test4": "4",
		},
	}

	go func() {
		test.mu.Lock()
		test.revMu.RLock()
		test.mapp["xxx1"] = "xxxx1"
		fmt.Println(test.mapp)

		test.revMu.RUnlock()

		test.mu.Unlock()
	}()

	go func() {
		test.mu.Lock()
		test.revMu.RLock()
		test.mapp["xxx"] = "xxxx"
		fmt.Println("++++", test.mapp)

		test.revMu.RUnlock()

		test.mu.Unlock()
	}()

	time.Sleep(time.Second * 1)
}
