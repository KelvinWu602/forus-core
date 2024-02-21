package main

import (
	"github.com/KelvinWu602/forus-core/p2p"
)

func main() {

	p2p.MakeServerAndStart(":3001")
	select {}
}
