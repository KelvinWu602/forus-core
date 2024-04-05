package main

import (
	"log"
	"os"
	"os/signal"

	"github.com/KelvinWu602/forus-core/p2p"
)

func main() {
	p2p.MakeServerAndStart()

	terminate := make(chan os.Signal, 1)
	signal.Notify(terminate, os.Interrupt)
	// wait for the SIGINT signal (Ctrl+C)
	log.Println("Press Ctrl+C to stop Forus-Core")
	<-terminate
}
