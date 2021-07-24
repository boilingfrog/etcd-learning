package main

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
)

type test struct {
	mu    sync.Mutex // guards fields below it
	revMu sync.RWMutex
	mapp  map[string]string
}

func main() {
	//test := test{
	//	mapp: map[string]string{
	//		"test1": "1",
	//		"test2": "2",
	//		"test3": "3",
	//		"test4": "4",
	//	},
	//}
	//
	//go func() {
	//	test.mu.Lock()
	//	test.revMu.RLock()
	//	test.mapp["xxx1"] = "xxxx1"
	//	fmt.Println(test.mapp)
	//
	//	test.revMu.RUnlock()
	//
	//	test.mu.Unlock()
	//}()
	//
	//go func() {
	//	test.mu.Lock()
	//	test.revMu.RLock()
	//	test.mapp["xxx"] = "xxxx"
	//	fmt.Println("++++", test.mapp)
	//
	//	test.revMu.RUnlock()
	//
	//	test.mu.Unlock()
	//}()
	//
	//time.Sleep(time.Second * 1)
	fmt.Println(DB("5c4d3d7f671427d02043e075"))
}

func DB(id string) (int, error) {
	if len(id) != 24 {
		return 0, errors.New("出错了")
	}
	key, err := strconv.ParseInt(id[len(id)-3:], 16, 0)
	if err != nil {
		return 0, errors.New("出错了")
	}
	i := int(key) % 28
	return i, nil
}
