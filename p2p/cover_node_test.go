package p2p

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/KelvinWu602/immutable-storage/blueprint"
	is "github.com/KelvinWu602/immutable-storage/protos"
	"github.com/KelvinWu602/immutable-storage/server"
	nd "github.com/KelvinWu602/node-discovery/protos"
	"google.golang.org/grpc"
)

// MockNodeDiscoveryServer implements node-discovery/protos/NodeDiscoveryServer interface
type MockNodeDiscoveryServer struct {
	nd.UnimplementedNodeDiscoveryServer
}

func (s MockNodeDiscoveryServer) JoinCluster(ctx context.Context, req *nd.JoinClusterRequest) (*nd.JoinClusterResponse, error) {
	return &nd.JoinClusterResponse{}, nil
}

func (s MockNodeDiscoveryServer) LeaveCluster(ctx context.Context, req *nd.LeaveClusterRequest) (*nd.LeaveClusterResponse, error) {
	return &nd.LeaveClusterResponse{}, nil
}

func (s MockNodeDiscoveryServer) GetMembers(ctx context.Context, req *nd.GetMembersRequest) (*nd.GetMembersReponse, error) {
	return &nd.GetMembersReponse{Member: []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}}, nil
}

// type message struct {
// 	// uuid is the first 16 bytes of the message key, which can be random values.
// 	uuid [16]byte
// 	// checksum is a SHA256 hash of concat(uuid,payload).
// 	checksum [32]byte
// 	// payload is the actual message content stored by upper application.
// 	payload []byte
// }

type Mock struct {
	cache map[blueprint.Key][]byte
}

func NewMock() *Mock {
	var m Mock
	m.cache = make(map[blueprint.Key][]byte)
	return &m
}

func (m *Mock) Store(key blueprint.Key, message []byte) error {
	if !blueprint.ValidateKey(key, message) {
		return errors.New("invalid key")
	}
	m.cache[key] = message
	return nil
}

func (m *Mock) Read(key blueprint.Key) ([]byte, error) {
	msg, found := m.cache[key]
	if found {
		return msg[48:], nil
	}
	return nil, errors.New("not discovered")
}

func (m *Mock) AvailableKeys() []blueprint.Key {
	result := make([]blueprint.Key, 0)
	for key := range m.cache {
		result = append(result, key)
	}
	return result
}

func (m *Mock) IsDiscovered(key blueprint.Key) bool {
	_, discovered := m.cache[key]
	return discovered
}

func initNodeDiscoveryMockServer(t *testing.T) *grpc.Server {
	lis, err := net.Listen("tcp", "localhost:3200")
	if err != nil {
		t.Logf("error when init MockNodeDiscoveryServer: %s", err)
		os.Exit(1)
	}
	gs := grpc.NewServer()
	mockServer := MockNodeDiscoveryServer{}
	nd.RegisterNodeDiscoveryServer(gs, mockServer)

	go func() {
		gs.Serve(lis)
	}()

	// Wait until the Mock Server is ready
	i := 1
	for conn, err := grpc.Dial("localhost:3200", grpc.WithInsecure()); err != nil; {
		log.Printf("Test call %v %s\n", i, err)
		if conn != nil {
			conn.Close()
		}
		time.Sleep(1 * time.Second)
		i++
	}

	return gs
}

func initImmutableStorageMockServer(t *testing.T) *grpc.Server {
	lis, err := net.Listen("tcp", "localhost:3100")
	if err != nil {
		t.Logf("error when init MockImmutableStorageServer: %s", err)
		os.Exit(1)
	}
	gs := grpc.NewServer()
	mock := NewMock()
	mockServer := server.NewApplicationServer(mock)
	is.RegisterImmutableStorageServer(gs, mockServer)

	go func() {
		gs.Serve(lis)
	}()

	// Wait until the Mock Server is ready
	i := 1
	for conn, err := grpc.Dial("localhost:3100", grpc.WithInsecure()); err != nil; {
		log.Printf("Test call %v %s\n", i, err)
		if conn != nil {
			conn.Close()
		}
		time.Sleep(1 * time.Second)
		i++
	}

	return gs
}

// try publish message
type TestPOSTMessageResponse struct {
	Msg    string `json:"message"`
	Err    string `json:"error"`
	NCover int    `json:"NUMBER_OF_COVER_NODES_FOR_PUBLISH,omitempty"`
	NPath  int    `json:"TARGET_NUMBER_OF_CONNECTED_PATHS,omitempty"`
	Covers int    `json:"number_of_covers,omitempty"`
	Paths  int    `json:"number_of_paths,omitempty"`
}

type TestGETMessageResponse struct {
	Content string `json:"content"`
}

var testkey = "n12BW68CQf443flTP8YIAE6r8Ah1oGmevAQW46_T5PuHjvzgVgn08cboOOjHTz1s"
var testmessage = "n12BW68CQf443flTP8YIAE6r8Ah1oGmevAQW46/T5PuHjvzgVgn08cboOOjHTz1sewoJCSJoZWxsbyI6ICIxIiwKCQkieW9vb28iOiAyCgl9"

func TestPublish(t *testing.T) {
	go initNodeDiscoveryMockServer(t)
	go initImmutableStorageMockServer(t)
	time.Sleep(2 * time.Second)
	var node *Node

	go func() {
		initConfigs()
		initGobTypeRegistration()
		node = NewNode()
		node.initDependencies()

		defer func() {
			node.ndClient.LeaveCluster()
		}()

		go node.StartTCP()
		// join cluster after TCP server is set up
		node.joinCluster()
		// after joined a cluster, start process user request
		go node.StartHTTP()

		go node.checkPublishConditionWorker()
		go node.maintainPathsHealthWorker()
	}()

	time.Sleep(2 * time.Second)
	os.Setenv("NUMBER_OF_COVER_NODES_FOR_PUBLISH", "0")
	os.Setenv("TARGET_NUMBER_OF_CONNECTED_PATHS", "1")

	t.Log(node.covers.getSize())

	// post path
	body := []byte(`{}`)
	resp0, err := http.Post("http://localhost:3000/path", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatal(err, "post path failed")
	}
	defer resp0.Body.Close()
	if resp0.StatusCode != http.StatusOK {
		t.Fatal(resp0.StatusCode, resp0.Status, "post path wrong status code")
	}

	// post message
	body = []byte(fmt.Sprintf(`{"content": "%v"}`, testmessage))
	resp1, err := http.Post(fmt.Sprintf("http://localhost:3000/message/%v", testkey), "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatal(err, "post message failed")
	}
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusCreated {
		errRespBody1 := TestPOSTMessageResponse{}
		derr := json.NewDecoder(resp1.Body).Decode(&errRespBody1)
		if derr != nil {
			t.Fatal(err, resp1.StatusCode, "decode post message error failed")
		}
		t.Fatal(err, resp1.StatusCode, "post message failed", errRespBody1)
	}
	t.Log("post path success")

	// get message
	resp2, err := http.Get(fmt.Sprintf("http://localhost:3000/message/%v", testkey))
	if err != nil {
		errRespBody2 := TestGETMessageResponse{}
		derr := json.NewDecoder(resp2.Body).Decode(&errRespBody2)
		if derr != nil {
			t.Fatal(err, "decode get message error failed")
		}
		t.Fatal(err, "get message failed", errRespBody2)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatal(resp2.StatusCode, resp2.Status, "get message wrong status code")
	}
	t.Log("get path success")
}

func TestValidateKey(t *testing.T) {
	key, _ := base64.URLEncoding.DecodeString("n12BW68CQf443flTP8YIAE6r8Ah1oGmevAQW46_T5PuHjvzgVgn08cboOOjHTz1s")
	message, _ := base64.StdEncoding.DecodeString("n12BW68CQf443flTP8YIAE6r8Ah1oGmevAQW46/T5PuHjvzgVgn08cboOOjHTz1sewoJCSJoZWxsbyI6ICIxIiwKCQkieW9vb28iOiAyCgl9")
	if !blueprint.ValidateKey(blueprint.Key(key[:]), message) {
		t.Fatal("should ok")
	}
}

func useTestMoveUpNodeConfigs() {
	// avoid fulfillPublishCondition
}

func TestMoveUpWhenNextHopNotProxySuccess(t *testing.T) {
	// expected to connect to next next hop

}

func TestMoveUpWhenNextHopNotProxyVerifyFail(t *testing.T) {
	// expected to self become proxy
}

func TestMoveUpWhenNextHopNotProxyVerifyOkConnectFail(t *testing.T) {
	// expected to self become proxy
}
