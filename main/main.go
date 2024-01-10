package main

import (
	"time"

	"github.com/KelvinWu602/forus-core/p2p"
)

func makeServerAndStart(addr string) *p2p.Node {
	s := p2p.New(addr)
	go s.Start()
	return s
}

func main() {

	makeServerAndStart(":3001") // n1
	makeServerAndStart(":3003") // n2
	n3 := makeServerAndStart(":3002")

	time.Sleep(1 * time.Second)
	// 3002 to 3001
	conn := n3.ConnectTo(":3001")
	time.Sleep(1 * time.Second)
	n3.QueryPath(conn)
	// a placeholder just to make something working
	select {}
}
