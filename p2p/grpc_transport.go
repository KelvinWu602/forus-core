package p2p

import (
	"context"
	"log"
	"time"

	"github.com/KelvinWu602/immutable-storage/blueprint"
	isProtos "github.com/KelvinWu602/immutable-storage/protos"
	ndProtos "github.com/KelvinWu602/node-discovery/protos"
	"google.golang.org/grpc"
)

// This handles connections with ND and IS
type NodeDiscoveryClient struct {
	client ndProtos.NodeDiscoveryClient
	// members []string
}

type ImmutableStorageClient struct {
	client isProtos.ImmutableStorageClient
}

func (nc *NodeDiscoveryClient) New() {
	conn, err := grpc.Dial(NODE_DISCOVERY_SERVER_LISTEN_PORT, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Cannot connect to Node Discovery: %s \n", err)
	}

	nc.client = ndProtos.NewNodeDiscoveryClient(conn)
}

func (nc *NodeDiscoveryClient) JoinCluster(ip string) (*ndProtos.JoinClusterResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return nc.client.JoinCluster(ctx, &ndProtos.JoinClusterRequest{ContactNodeIP: ip})
}

func (nc *NodeDiscoveryClient) LeaveCluster() (*ndProtos.LeaveClusterResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return nc.client.LeaveCluster(ctx, &ndProtos.LeaveClusterRequest{})
}

func (nc *NodeDiscoveryClient) GetMembers() (*ndProtos.GetMembersReponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return nc.client.GetMembers(ctx, &ndProtos.GetMembersRequest{})
}

func (ic *ImmutableStorageClient) New() {
	conn, err := grpc.Dial(IMMUTABLE_STORAGE_SERVER_LISTEN_PORT, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Cannot connect to Immutable Storage: %s \n", err)
	}
	ic.client = isProtos.NewImmutableStorageClient(conn)
}

func (ic *ImmutableStorageClient) Store(key blueprint.Key, body []byte) (*isProtos.StoreResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return ic.client.Store(ctx, &isProtos.StoreRequest{Key: key[:], Content: body})
}

func (ic *ImmutableStorageClient) Read(key []byte) (*isProtos.ReadResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return ic.client.Read(ctx, &isProtos.ReadRequest{Key: key})
}

func (ic *ImmutableStorageClient) AvailableIDs() (*isProtos.AvailableKeysResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return ic.client.AvailableKeys(ctx, &isProtos.AvailableKeysRequest{})
}

func (ic *ImmutableStorageClient) IsDiscovered(key []byte) (*isProtos.IsDiscoveredResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return ic.client.IsDiscovered(ctx, &isProtos.IsDiscoveredRequest{Key: key})
}

func initNodeDiscoverClient() *NodeDiscoveryClient {
	nd := &NodeDiscoveryClient{}
	nd.New()
	_, err := nd.GetMembers()
	if err != nil {
		log.Fatalf("Cannot get member from node discovery %s \n", err)
	}

	// iterate until the first alive member
	// nd.members = resp.Member
	return nd
}

func initImmutableStorageClient() *ImmutableStorageClient {
	is := &ImmutableStorageClient{}
	is.New()
	return is
}
