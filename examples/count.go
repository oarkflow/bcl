package main

import (
	"fmt"
	"time"
)

func main() {
	n := 0
	start := time.Now()
	for i := 0; i < 10000000000; i++ {
		n += i
	}
	fmt.Println("time:", time.Since(start))
}
