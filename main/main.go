package main

import (
	"log"

	"github.com/KelvinWu602/forus-core/p2p"
)

func main() {

	n1 := p2p.New(":3001")

	// this go func encapsulates all actions on node N1
	go func() {
		go n1.Start()
		log.Println("check for blocks on 3001")
	}()

	n3 := p2p.New(":3002")

	// this go func encapsulates all actions on node N3
	go func() {
		go n3.Start()
		n3.QueryPath(":3001")
	}()

	// a placeholder just to make something working
	select {}
}
