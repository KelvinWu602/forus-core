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

func (nc *NodeDiscoveryClient) New() error {
	addr := "localhost" + NODE_DISCOVERY_SERVER_LISTEN_PORT
	conn, err := grpc.Dial(addr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("Cannot connect to Node Discovery: %s \n", err)
		return err
	}
	nc.client = ndProtos.NewNodeDiscoveryClient(conn)
	return nil
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

func (ic *ImmutableStorageClient) New() error {
	conn, err := grpc.Dial(IMMUTABLE_STORAGE_SERVER_LISTEN_PORT, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("Cannot connect to Immutable Storage: %s \n", err)
		return err
	}
	ic.client = isProtos.NewImmutableStorageClient(conn)
	return nil
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
	logMsg("initNodeDiscoverClient", "connecting to Node-Discovery...")
	nd := &NodeDiscoveryClient{}
	err := nd.New()
	for err != nil {
		logError("initNodeDiscoverClient", err, "Retry connect to Node-Discovery after 1 second...")
		time.Sleep(time.Second)
		err = nd.New()
	}
	return nd
}

func initImmutableStorageClient() *ImmutableStorageClient {
	logMsg("initImmutableStorageClient", "connecting to Immutable-Storage...")
	is := &ImmutableStorageClient{}
	err := is.New()
	for err != nil {
		logError("initImmutableStorageClient", err, "Retry connect to Immutable-Storage after 1 second...")
		time.Sleep(time.Second)
		err = is.New()
	}
	return is
}
