package main

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	pb "proto"
	"io"
	"net"
	"fmt"
)

type wardenServer struct {
	clusters map[string]pb.ClusterAdvertisement
	clients  map[*pb.ClusterClientService_ServerClustersServer]bool
	requests chan *pb.ClusterRequest
}

// setup an internal map of all available cluster resources (clusterId -> cluster)

// setup a queue for incoming requests from the clients
//    - queue will be served by a worker that applies some "policy" / business logic and
//      relays the requests to one of the selected agents

func (s *wardenServer) sendExisting(stream pb.ClusterClientService_ServerClustersServer) {
	stream.Send(&pb.ClusterAdvertisement{
		ClusterId: "bar",
	})
	for _, cluster := range s.clusters {
		stream.Send(&cluster)
	}
}

func (s *wardenServer) ServerClusters(stream pb.ClusterClientService_ServerClustersServer) error {
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

func (s *wardenServer) AgentClusters(stream pb.ClusterAgentService_AgentClustersServer) error {
	// register the stream into the inventory of active agents
	// setup polling loop for receiving new cluster advertisements

	// when new advertisement updates come in, update the in-memory structures and relay the
	// message about the updated resource

	// defer mechanism to prune the inventory
	return nil
}

func newServer() *wardenServer {
	s := new(wardenServer)
	s.clusters = make(map[string]pb.ClusterAdvertisement)
	s.clients = make(map[*pb.ClusterClientService_ServerClustersServer]bool)
	s.requests = make(chan *pb.ClusterRequest)

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
	pb.RegisterClusterClientServiceServer(grpcServer, newServer())
	fmt.Println("starting to serve...")
	grpcServer.Serve(lis)
}
