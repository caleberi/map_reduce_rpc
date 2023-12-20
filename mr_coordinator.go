package main

import (
	"fmt"
	"os"
	"time"

	"github.com/caleberi/map_reduce_rpc/mr"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: mrcoordinator inputfiles...\n")
		os.Exit(1)
	}

	c := mr.NewCoordinator(os.Args[1:], 10, 10*time.Second)
	for c.Done() == false {
		time.Sleep(time.Second)
	}

	time.Sleep(time.Second)
}
