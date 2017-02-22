package main

import (
	"fmt"
	"github.com/opennetworkinglab/onos-warden/warden"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"io"
	"net"
)

type wardenServer struct {
	clusters map[string]warden.ClusterAdvertisement
	clients  map[*warden.ClusterClientService_ServerClustersServer]bool
	agents   map[*warden.ClusterAgentService_AgentClustersServer]bool
	requests chan *warden.ClusterRequest
}

// setup an internal map of all available cluster resources (clusterId -> cluster)

// setup a queue for incoming requests from the clients
//    - queue will be served by a worker that applies some "policy" / business logic and
//      relays the requests to one of the selected agents

func (s *wardenServer) sendExisting(stream warden.ClusterClientService_ServerClustersServer) {
	stream.Send(&warden.ClusterAdvertisement{
		ClusterId: "bar",
	})
	for _, cluster := range s.clusters {
		stream.Send(&cluster)
	}
}

func (s *wardenServer) ServerClusters(stream warden.ClusterClientService_ServerClustersServer) error {
	// register the stream so that we can send it new information to all active clients
	s.clients[&stream] = true

	// we can use the defer mechanism to prune the stream
	defer delete(s.clients, &stream)

	// send what we have, i.e. send them existing clusters
	s.sendExisting(stream)

	// setup a go routing that will poll for requests from the client
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		s.requests <- in
	}
	return nil
}

func (s *wardenServer) AgentClusters(stream warden.ClusterAgentService_AgentClustersServer) error {
	// register the stream into the inventory of active agents
	// setup polling loop for receiving new cluster advertisements

	// when new advertisement updates come in, update the in-memory structures and relay the
	// message about the updated resource

	// defer mechanism to prune the inventory
	fmt.Printf("%v\n", stream)
	return nil
}

func newServer() *wardenServer {
	s := new(wardenServer)
	s.clusters = make(map[string]warden.ClusterAdvertisement)
	s.clients = make(map[*warden.ClusterClientService_ServerClustersServer]bool)
	s.agents = make(map[*warden.ClusterAgentService_AgentClustersServer]bool)
	s.requests = make(chan *warden.ClusterRequest)

	go func() {
		for {
			request := <-s.requests
			fmt.Println(request)
		}
	}()
	return s
}

func main() {
	lis, err := net.Listen("tcp", ":1234")
	if err != nil {
		grpclog.Fatalf("failed to listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	s := newServer()
	warden.RegisterClusterClientServiceServer(grpcServer, s)
	warden.RegisterClusterAgentServiceServer(grpcServer, s)
	fmt.Println("starting to serve...")
	grpcServer.Serve(lis)
}
