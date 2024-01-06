package main

import (
	"github.com/KelvinWu602/forus-core/p2p"
)

func main() {

	n1 := p2p.New(":3001")
	n1.Start()

	n3 := p2p.New(":3002")
	n3.Start()
	n3.QueryPath(":3001")

	// a placeholder just to make something working
	select {}
}
