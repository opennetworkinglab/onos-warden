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

type request struct {
	req    *warden.ClusterRequest
	client warden.ClusterClientService_ServerClustersServer
}

type key struct {
	cId   string
	cType string
}

type wardenServer struct {
	lock sync.Mutex

	// mapping from key(ClusterId, ClusterType) to cluster resources
	clusters map[key]cluster

	// mapping from RequestId to key(ClusterId, ClusterType)
	requests map[string]key

	// registries of client and agent streams
	clients map[warden.ClusterClientService_ServerClustersServer]bool
	agents  map[warden.ClusterAgentService_AgentClustersServer]bool

	// setup a queue for incoming requests from the client
	//    - queue will be served by a worker that applies some "policy" / business logic and
	//      relays the requests to one of the selected agent
	incomingReq chan request
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

	//FIXME revoke all duration == -1 requests if client disconnects

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
		s.incomingReq <- request{in, stream}
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

		// remove cells from the warden map when agent disappears
		for id, cl := range s.clusters {
			//TODO maybe we should time these out instead? in case, the agent is coming right back
			if cl.agent == stream {
				delete(s.clusters, id)
			}
			if rId := cl.ad.RequestId; rId != "" {
				delete(s.requests, rId)
				//TODO need to send UNAVAILABLE
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
			fmt.Println("agent stream error", stream, err)
			return err
		}
		fmt.Println(in)

		s.lock.Lock()

		// update the in-memory structures
		k := key{in.ClusterId, in.ClusterType}
		existing, ok := s.clusters[k]
		if ok && in.RequestId != existing.ad.RequestId {
			// reservation is no longer assocated with the old request
			delete(s.requests, existing.ad.RequestId)
		}
		s.clusters[k] = cluster{in, stream}
		if in.RequestId != "" {
			s.requests[in.RequestId] = k
		}

		fmt.Println(s.clusters)
		fmt.Println(s.requests)

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
		request := <-s.incomingReq
		fmt.Println()
		req := request.req
		client := request.client

		func() {
			s.lock.Lock()
			defer s.lock.Unlock()

			// Check to see if we have already satisfied the request
			rId := req.RequestId
			k, ok := s.requests[rId]
			if ok {
				ad, ok := s.clusters[k]
				if ok {
					// unicast the cluster to client
					fmt.Println("send ad to client", ad)
					client.Send(ad.ad)
					ad.agent.Send(req)
					return
				} else {
					// this shouldn't happen, but we'll do some cleanup if it does
					delete(s.requests, rId)
				}
			}

			// If not, find an appropriate cluster for the request
			for _, c := range s.clusters {
				if req.ClusterType != "" && req.ClusterType != c.ad.ClusterType {
					continue
				}
				if req.ClusterId != "" && req.ClusterId != c.ad.ClusterId {
					continue
				}
				// find the first one that is available
				if c.ad.State == warden.ClusterAdvertisement_AVAILABLE {
					// relay the request to the agent that advertised it
					fmt.Println("sending request to cluster", c, req)
					c.agent.Send(req)
					s.requests[rId] = key{c.ad.ClusterId, c.ad.ClusterType}
					return
				}
			}
		}()

	}
}

func newServer() *wardenServer {
	s := new(wardenServer)
	s.clusters = make(map[key]cluster)
	s.requests = make(map[string]key)
	s.clients = make(map[warden.ClusterClientService_ServerClustersServer]bool)
	s.agents = make(map[warden.ClusterAgentService_AgentClustersServer]bool)
	s.incomingReq = make(chan request)
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
