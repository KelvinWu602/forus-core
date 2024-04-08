package p2p

import (
	"context"
	"log"
	"net"
	"testing"
	"time"

	is "github.com/KelvinWu602/immutable-storage/protos"
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

func (s MockNodeDiscoveryServer) mustEmbedUnimplementedNodeDiscoveryServer() {
}

// MockNodeDiscoveryServer implements node-discovery/protos/NodeDiscoveryServer interface
type MockImmutableStorageServer struct {
	is.UnimplementedImmutableStorageServer
}

func (s MockImmutableStorageServer) Store(context.Context, *is.StoreRequest) (*is.StoreResponse, error) {
	return nil, nil
}
func (s MockImmutableStorageServer) Read(context.Context, *is.ReadRequest) (*is.ReadResponse, error) {
	return nil, nil
}
func (s MockImmutableStorageServer) AvailableKeys(context.Context, *is.AvailableKeysRequest) (*is.AvailableKeysResponse, error) {
	return nil, nil
}
func (s MockImmutableStorageServer) IsDiscovered(ctx context.Context, req *is.IsDiscoveredRequest) (*is.IsDiscoveredResponse, error) {
	return &is.IsDiscoveredResponse{IsDiscovered: false}, nil
}

func (s MockImmutableStorageServer) mustEmbedUnimplementedImmutableStorageServer() {
}

func initNodeDiscoveryMockServer(t *testing.T) *grpc.Server {
	lis, err := net.Listen("tcp", "localhost:3200")
	if err != nil {
		t.Fatalf("error when init MockNodeDiscoveryServer: %s", err)
	}
	gs := grpc.NewServer()
	var mockServer MockNodeDiscoveryServer
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
		t.Fatalf("error when init MockImmutableStorageServer: %s", err)
	}
	gs := grpc.NewServer()
	var mockServer MockImmutableStorageServer
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

func TestWhenNextHopDied(t *testing.T) {

}
