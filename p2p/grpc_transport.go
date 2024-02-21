package p2p

import (
	"context"
	"log"
	"time"

	"github.com/KelvinWu602/node-discovery/protos"
	"google.golang.org/grpc"
)

// This handles connections with ND and IS

const (
	NDAddr = ":3200"
)

type NodeDiscoveryClient struct {
	client  protos.NodeDiscoveryClient
	members []string
}

type ImmutableStorageClient struct {
}

func (nc *NodeDiscoveryClient) New() {
	conn, err := grpc.Dial(NDAddr)
	if err != nil {
		log.Fatalf("Dial grpc failed %s \n", err)
	}

	nc.client = protos.NewNodeDiscoveryClient(conn)
}

func (nc *NodeDiscoveryClient) JoinCluster(ip string) (*protos.JoinClusterResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return nc.client.JoinCluster(ctx, &protos.JoinClusterRequest{ContactNodeIP: ip})
}

func (nc *NodeDiscoveryClient) LeaveCluster() (*protos.LeaveClusterResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return nc.client.LeaveCluster(ctx, &protos.LeaveClusterRequest{})
}

func (nc *NodeDiscoveryClient) GetMembers() (*protos.GetMembersReponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return nc.client.GetMembers(ctx, &protos.GetMembersRequest{})
}

func initNodeDiscoverClient() *NodeDiscoveryClient {
	nd := &NodeDiscoveryClient{}
	nd.New()
	resp, err := nd.GetMembers()
	if err != nil {
		log.Fatalf("Cannot get member from node discovery %s \n", err)
	}

	// iterate until the first alive member
	nd.members = resp.Member
	return nd
}
