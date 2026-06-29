package main

import (
	"fmt"
	"strconv"
)

func main() {
	capacity := 10.0
	refillPerSecond := 10.0 / 30.0
	ttl := int64(30)
	
	fmt.Printf("cap: %s\n", strconv.FormatFloat(capacity, 'f', -1, 64))
	fmt.Printf("refill: %s\n", strconv.FormatFloat(refillPerSecond/1000.0, 'g', -1, 64))
	fmt.Printf("ttl: %s\n", strconv.FormatInt(ttl, 10))
}
