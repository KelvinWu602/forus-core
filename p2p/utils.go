package p2p

import (
	"math/rand"
	"sync"
)

func pick[K comparable, V any](m map[K]V, lock *sync.RWMutex) (K, V) {
	if lock != nil {
		lock.RLock()
		defer lock.RUnlock()
	}
	i := rand.Intn(len(m))
	for k, v := range m {
		if i == 0 {
			return k, v
		}
		i--
	}
	panic("unreachable")
}
