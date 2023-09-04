package main

import (
	"fmt"

	"github.com/KelvinWu602/forus-core/message"
)

func main() {
	var m message.T2Message
	m.JobID = 1
	m.ProxyID = 2
	m.Salt = 3
	m.Content[0] = 4
	msg := m.ToBytes()
	fmt.Println(msg)
}
