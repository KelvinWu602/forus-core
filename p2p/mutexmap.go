package p2p

import "sync"

type MutexMap[K comparable, V any] struct {
	data map[K]V
	lock *sync.RWMutex
}

func NewMutexMap[K comparable, V any]() *MutexMap[K, V] {
	return &MutexMap[K, V]{
		data: make(map[K]V),
		lock: &sync.RWMutex{},
	}
}

func (m *MutexMap[K, V]) getValue(key K) (V, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	value, found := m.data[key]
	return value, found
}

func (m *MutexMap[K, V]) getSize() int {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return len(m.data)
}

func (m *MutexMap[K, V]) setValue(key K, value V) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.data[key] = value
}

func (m *MutexMap[K, V]) deleteValue(key K) {
	m.lock.Lock()
	defer m.lock.Unlock()
	delete(m.data, key)
}

func (m *MutexMap[K, V]) iterate(process func(K, V), readOnly bool) {
	if readOnly {
		m.lock.RLock()
		defer m.lock.RUnlock()
	} else {
		m.lock.Lock()
		defer m.lock.Unlock()
	}

	for key, value := range m.data {
		process(key, value)
	}
}
