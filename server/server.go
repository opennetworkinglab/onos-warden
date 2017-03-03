package main

import (
	"fmt"
	"github.com/opennetworkinglab/onos-warden/warden"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"io"
	"net"
	"sync"
)

type cluster struct {
	ad    *warden.ClusterAdvertisement
	agent warden.ClusterAgentService_AgentClustersServer
}

type wardenServer struct {
	lock sync.Mutex

	// setup an internal map of all available cluster resources (clusterId -> cluster)
	clusters map[string]cluster

	// registries of client and agent streams
	clients map[warden.ClusterClientService_ServerClustersServer]bool
	agents  map[warden.ClusterAgentService_AgentClustersServer]bool

	// setup a queue for incoming requests from the client
	//    - queue will be served by a worker that applies some "policy" / business logic and
	//      relays the requests to one of the selected agent
	requests chan *warden.ClusterRequest
}

func (s *wardenServer) ServerClusters(stream warden.ClusterClientService_ServerClustersServer) error {
	s.lock.Lock()

	// register the stream so that we can send it new information to all active client
	s.clients[stream] = true

	// we can use the defer mechanism to prune the stream
	defer delete(s.clients, stream)

	// send what we have, i.e. send them existing clusters
	for _, cluster := range s.clusters {
		fmt.Println("Sending update", stream, cluster.ad)
		stream.Send(cluster.ad)
	}

	s.lock.Unlock()

	// setup a go routing that will poll for requests from the client
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			fmt.Println("client stream closed", stream)
			return nil
		}
		if err != nil {
			fmt.Println("client stream error", stream)
			return err
		}

		// enqueue the request
		s.requests <- in
	}
	return nil
}

func (s *wardenServer) AgentClusters(stream warden.ClusterAgentService_AgentClustersServer) error {
	s.lock.Lock()

	// register the stream into the inventory of active agent
	s.agents[stream] = true

	s.lock.Unlock()

	// defer mechanism to prune the inventory
	defer func() {
		s.lock.Lock()
		defer s.lock.Unlock()

		// remove cells from the wardne map when agent disappears
		for id, cl := range s.clusters {
			//TODO maybe we should time these out instead?
			if cl.agent == stream {
				delete(s.clusters, id)
			}
		}
		delete(s.agents, stream)
		fmt.Println(s.clusters, s.agents)
	}()

	// setup polling loop for receiving new cluster advertisements
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			fmt.Println("agent stream closed", stream)
			return nil
		}
		if err != nil {
			fmt.Println("agent stream error", stream)
			return err
		}
		fmt.Println(in)

		s.lock.Lock()

		// update the in-memory structures
		s.clusters[in.ClusterId] = cluster{in, stream}
		fmt.Println(s.clusters)

		// relay the message about the updated resource
		for c := range s.clients {
			fmt.Println("Sending update", c, in)
			c.Send(in)
		}

		s.lock.Unlock()
	}

	return nil
}

func (s *wardenServer) processRequests() {
	for {
		request := <-s.requests
		fmt.Println(request)

		// find cluster
		for _, c := range s.clusters {
			// find the first one that is available
			if c.ad.State == warden.ClusterAdvertisement_AVAILABLE {
				// relay the request to the agent that advertised it
				c.agent.Send(request)
				fmt.Println("sending request to cluster", c, request)
				break
			}
		}
	}
}

func newServer() *wardenServer {
	s := new(wardenServer)
	s.clusters = make(map[string]cluster)
	s.clients = make(map[warden.ClusterClientService_ServerClustersServer]bool)
	s.agents = make(map[warden.ClusterAgentService_AgentClustersServer]bool)
	s.requests = make(chan *warden.ClusterRequest)
	return s
}

func main() {
	lis, err := net.Listen("tcp", ":1234")
	if err != nil {
		grpclog.Fatalf("failed to listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	s := newServer()
	go s.processRequests()
	warden.RegisterClusterClientServiceServer(grpcServer, s)
	warden.RegisterClusterAgentServiceServer(grpcServer, s)
	fmt.Println("starting to serve...")
	grpcServer.Serve(lis)
}
