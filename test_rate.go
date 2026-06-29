package main

import (
	"fmt"
	"time"
	"golang.org/x/time/rate"
)

func main() {
	l := rate.NewLimiter(1, 1)
	fmt.Printf("tokens: %f\n", l.TokensAt(time.Now()))
}
