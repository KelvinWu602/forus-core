package p2p

import (
	"errors"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/KelvinWu602/immutable-storage/blueprint"
	is "github.com/KelvinWu602/immutable-storage/protos"
	nd "github.com/KelvinWu602/node-discovery/protos"

	iss "github.com/KelvinWu602/immutable-storage/server"
	nds "github.com/KelvinWu602/node-discovery/server"

	"google.golang.org/grpc"
)

// MockND implements node-discovery/protos/NodeDiscovery interface
type MockND struct {
	joined  bool
	members []string
}

func NewMockND(members []string) *MockND {
	return &MockND{joined: false, members: members}
}

// communicate with a node specified by input IP address and join the cluster of that node. In case of any errors, log it and return “failed to join cluster” error.
func (nd *MockND) JoinCluster(_ string) error {
	nd.joined = true
	return nil
}

// notify other nodes in the cluster that you are going to leave. Log it and return “failed to leave cluster” error.
func (nd *MockND) LeaveCluster() error {
	nd.joined = false
	return nil
}

// Acquire the array of the IP addresses of all cluster members. These IP addresses are assumed to be alive.
// Return error “node discovery service aborted” if underlying service is not running. Return error “failed to get member ip” otherwise.
func (nd *MockND) GetMembers() ([]string, error) {
	if nd.joined {
		return nd.members, nil
	} else {
		return nil, nil
	}
}

// MockND implements node-discovery/protos/NodeDiscovery interface
type MockIS struct {
	lock  *sync.Mutex
	cache map[blueprint.Key][]byte
}

func NewMockIS() *MockIS {
	return &MockIS{
		lock:  &sync.Mutex{},
		cache: make(map[blueprint.Key][]byte),
	}
}

func ShareCacheWith(other *MockIS) *MockIS {
	return &MockIS{
		lock:  other.lock,
		cache: other.cache,
	}
}

func (m *MockIS) Store(key blueprint.Key, message []byte) error {
	if !blueprint.ValidateKey(key, message) {
		return errors.New("invalid key")
	}
	m.lock.Lock()
	defer m.lock.Unlock()
	m.cache[key] = message
	return nil
}

func (m *MockIS) Read(key blueprint.Key) ([]byte, error) {
	m.lock.Lock()
	defer m.lock.Unlock()
	msg, found := m.cache[key]
	if found {
		return msg[48:], nil
	}
	return nil, errors.New("not discovered")
}

func (m *MockIS) AvailableKeys() []blueprint.Key {
	m.lock.Lock()
	defer m.lock.Unlock()
	result := make([]blueprint.Key, 0)
	for key := range m.cache {
		result = append(result, key)
	}
	return result
}

func (m *MockIS) IsDiscovered(key blueprint.Key) bool {
	m.lock.Lock()
	defer m.lock.Unlock()
	_, discovered := m.cache[key]
	return discovered
}

func initMockIS(mock *MockIS, port string) *grpc.Server {
	lis, err := net.Listen("tcp", "localhost:"+port)
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
	gs := grpc.NewServer()
	mockServer := iss.NewApplicationServer(mock)
	is.RegisterImmutableStorageServer(gs, mockServer)

	go func() {
		gs.Serve(lis)
	}()

	// Wait until the Mock Server is ready
	i := 1
	for conn, err := grpc.Dial("localhost"+port, grpc.WithInsecure()); err != nil; {
		log.Printf("Test call %v %s\n", i, err)
		if conn != nil {
			conn.Close()
		}
		time.Sleep(1 * time.Second)
		i++
	}

	return gs
}

func initMockND(mock *MockND, port string) *grpc.Server {
	lis, err := net.Listen("tcp", "localhost:"+port)
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
	gs := grpc.NewServer()
	mockServer := nds.NewServer(mock)
	nd.RegisterNodeDiscoveryServer(gs, mockServer)

	go func() {
		gs.Serve(lis)
	}()

	// Wait until the Mock Server is ready
	i := 1
	for conn, err := grpc.Dial("localhost"+port, grpc.WithInsecure()); err != nil; {
		log.Printf("Test call %v %s\n", i, err)
		if conn != nil {
			conn.Close()
		}
		time.Sleep(1 * time.Second)
		i++
	}

	return gs
}
