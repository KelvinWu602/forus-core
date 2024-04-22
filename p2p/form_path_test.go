package p2p

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMockNDJoin(t *testing.T) {
	assert := assert.New(t)
	members := map[string][]string{}
	members["127.0.0.1"] = []string{"127.0.0.2", "127.0.0.3"}
	members["127.0.0.2"] = []string{"127.0.0.1", "127.0.0.3"}
	members["127.0.0.3"] = []string{"127.0.0.1", "127.0.0.2"}
	members["127.0.0.4"] = []string{}

	NDlock := sync.RWMutex{}
	mockND4 := NewMockND("127.0.0.4", members, &NDlock)

	assert.Equal(nil, mockND4.JoinCluster("127.0.0.1"), "should return no error")
	others, err := mockND4.GetMembers()
	assert.Equal(nil, err, "should return no error")
	assert.Equal(3, len(others), "should return 3 members")
	assert.ElementsMatch([]string{"127.0.0.1", "127.0.0.2", "127.0.0.3"}, others, "should return 3 members")
}

// go test -timeout 240s -run ^TestTreeFormation$ github.com/KelvinWu602/forus-core/p2p -v -count=1
func TestTreeFormation(t *testing.T) {
	// node ip          target # of paths
	// N1   127.0.0.1   0
	// N2   127.0.0.2   0
	// N3   127.0.0.3   0
	// N4   127.0.0.4   2			contact 127.0.0.1 on start

	assert := assert.New(t)
	var setup sync.WaitGroup
	setup.Add(5)
	// All component prefixed with 1 ==> Mock
	// All component prefixed with 2 ==> Normal
	members := map[string][]string{}
	members["127.0.0.1"] = []string{"127.0.0.2", "127.0.0.3"}
	members["127.0.0.2"] = []string{"127.0.0.1", "127.0.0.3"}
	members["127.0.0.3"] = []string{"127.0.0.1", "127.0.0.2"}
	members["127.0.0.4"] = []string{}

	NDlock := sync.RWMutex{}
	mockND1 := NewMockND("127.0.0.1", members, &NDlock)
	mockND2 := NewMockND("127.0.0.2", members, &NDlock)
	mockND3 := NewMockND("127.0.0.3", members, &NDlock)
	mockND4 := NewMockND("127.0.0.4", members, &NDlock)

	mockIS1 := NewMockIS()
	mockIS2 := ShareCacheWith(mockIS1)
	mockIS3 := ShareCacheWith(mockIS1)
	mockIS4 := ShareCacheWith(mockIS1)

	var N4 *Node
	go func() {
		initMockND(mockND1, "127.0.0.1:3200")
		initMockND(mockND2, "127.0.0.2:3200")
		initMockND(mockND3, "127.0.0.3:3200")
		initMockND(mockND4, "127.0.0.4:3200")

		initMockIS(mockIS1, "127.0.0.1:3100")
		initMockIS(mockIS2, "127.0.0.2:3100")
		initMockIS(mockIS3, "127.0.0.3:3100")
		initMockIS(mockIS4, "127.0.0.4:3100")

		setup.Done()
	}()

	go func() {
		_ = StartNodeInternal("../unit_test_configs/config-TestTreeFormation-N1.yaml")
		setup.Done()
	}()

	go func() {
		_ = StartNodeInternal("../unit_test_configs/config-TestTreeFormation-N2.yaml")
		setup.Done()
	}()

	go func() {
		_ = StartNodeInternal("../unit_test_configs/config-TestTreeFormation-N3.yaml")
		setup.Done()
	}()

	go func() {
		N4 = StartNodeInternal("../unit_test_configs/config-TestTreeFormation-N4.yaml")
		setup.Done()
	}()
	setup.Wait()

	// give N4 some time
	time.Sleep(20 * time.Second)

	assert.Equal(2, N4.paths.getSize(), "should have formed 2 paths")
}
