package main

import (
	"fmt"
	"os"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "paq: unexpected error: %v\n", r)
			os.Exit(2)
		}
	}()
	Execute()
}
