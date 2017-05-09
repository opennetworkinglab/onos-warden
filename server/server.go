package main

import (
	"fmt"
	"github.com/opennetworkinglab/onos-warden/warden"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/peer"
	"io"
	"net"
	"sync"
	"time"
	"errors"
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

type recvAd interface {
	Send(*warden.ClusterAdvertisement) error
	Context() context.Context
}

type recvReq interface {
	Send(*warden.ClusterRequest) error
	Context() context.Context
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

	// registries of channels waiting for a cluster to be ready
	waiters map[key][]chan *warden.ClusterAdvertisement
}

func keyFromCluster(cl *cluster) key {
	return key{cl.ad.ClusterId, cl.ad.ClusterType}
}

func logClient(ctx context.Context, t string, o interface{}) {
	logConn(ctx, "Client", t, o)
}

func logAgent(ctx context.Context, t string, o interface{}) {
	logConn(ctx, "Agent", t, o)
}

func logConn(ctx context.Context, a, t string, o interface{}) {
	var pStr string
	p, ok := peer.FromContext(ctx)
	if !ok {
		pStr = "<unknown>"
	} else {
		pStr = fmt.Sprintf("%v (auth: %v)", p.Addr, p.AuthInfo)
	}
	if o != nil {
		fmt.Printf("%s: %s %s: %v\n", a, t, pStr, o)
	} else {
		fmt.Printf("%s: %s %s\n", a, t, pStr)
	}
}

func (s *wardenServer) Request(ctx context.Context, req *warden.ClusterRequest) (ad *warden.ClusterAdvertisement, err error) {
	logClient(ctx, "New request from", req)
	wait, err := s.processRequest(req)
	if err != nil {
		fmt.Printf("Error processing request %v\n%v\n", req, err)
		return nil, err
	}
	ad = <-wait
	if ad == nil {
		return nil, errors.New("Unable to process request")
	}
	logClient(ctx, "Sending ad to", ad)
	return ad, nil
}

func (s *wardenServer) sendSnapshot(c recvAd) error {
	// Note: callers to this method should hold s.lock
	for _, cluster := range s.clusters {
		err := c.Send(cluster.ad)
		if err != nil {
			return err
		} else {
			logClient(c.Context(), "Sending ad (snapshot) to", cluster.ad)
		}
	}
	return nil
}

func (s *wardenServer) List(_ *warden.Empty, stream warden.ClusterClientService_ListServer) error {
	logClient(stream.Context(), "List from", nil)
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.sendSnapshot(stream)
}

func (s *wardenServer) ServerClusters(stream warden.ClusterClientService_ServerClustersServer) error {
	logClient(stream.Context(), "New stream from", nil)
	s.lock.Lock()
	// register the stream so that we can send it new information to all active client
	s.clients[stream] = true

	// we can use the defer mechanism to prune the stream when the stream is closed or encounters an error
	defer func() {
		s.lock.Lock()
		defer s.lock.Unlock()
		delete(s.clients, stream)
		//FIXME revoke all duration == -1 requests if client disconnects
	}()

	err := s.sendSnapshot(stream)
	s.lock.Unlock()
	if err != nil {
		return err
	}

	// Poll for requests from the client
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			logClient(stream.Context(), "EOF (close) from", nil)
			return nil
		}
		if err != nil {
			logClient(stream.Context(), "Connection error from", err)
			return err
		}
		go s.processRequest(req)
	}
	return nil
}

func (s *wardenServer) sendUpdate(ad *warden.ClusterAdvertisement) {
	// Note: callers to this method should hold s.lock
	for c := range s.clients {
		err := c.Send(ad)
		if err != nil {
			logClient(c.Context(), "Error sending update to", err)
		} else {
			logClient(c.Context(), "Sent update to", ad)
		}
	}
}

func (s *wardenServer) updateCluster(cl *cluster) {
	// Note: callers must hold s.lock
	k := keyFromCluster(cl)
	existing, ok := s.clusters[k]
	if ok && cl.ad.RequestId != existing.ad.RequestId {
		// reservation is no longer assocated with the old request; delete the mapping
		delete(s.requests, existing.ad.RequestId)
	}
	s.clusters[k] = *cl
	if cl.ad.RequestId != "" {
		// update the request mapping (this is conservative, and likely won't change anything
		s.requests[cl.ad.RequestId] = k
	}

	// If the cluster is ready, update all local waiters for this cluster
	if cl.ad.State == warden.ClusterAdvertisement_READY {
		w, ok := s.waiters[k]
		if ok {
			for _, ch := range w {
				ch <- cl.ad
			}
			// Remove the waiters, now that they have been updated
			delete(s.waiters, k)
		}
	}

	// Send update to all streaming clients
	s.sendUpdate(cl.ad)
}

func (s *wardenServer) deleteCluster(cl *cluster) {
	// Note: callers must hold s.lock
	k := keyFromCluster(cl)
	delete(s.clusters, k)
	if rId := cl.ad.RequestId; rId != "" {
		delete(s.requests, rId)
		//TODO need to send UNAVAILABLE
	}

	// Close all local waiters for this cluster
	w, ok := s.waiters[k]
	if ok {
		for _, ch := range w {
			close(ch)
		}
		// Remove the waiters, now that they have been closed
		delete(s.waiters, k)
	}

}

func (s *wardenServer) AgentClusters(stream warden.ClusterAgentService_AgentClustersServer) error {
	logAgent(stream.Context(), "New stream from", nil)

	s.lock.Lock()
	// register the stream into the inventory of active agent
	s.agents[stream] = true
	s.lock.Unlock()

	// defer mechanism to prune the inventory
	defer func() {
		s.lock.Lock()
		defer s.lock.Unlock()

		// remove cells from the warden map when agent disappears
		for _, cl := range s.clusters {
			//TODO maybe we should time these out instead? in case, the agent is coming right back
			if cl.agent == stream {
				s.deleteCluster(&cl)
			}
		}
		delete(s.agents, stream)
	}()

	// setup polling loop for receiving new cluster advertisements
	for {
		cl, err := stream.Recv()
		if err == io.EOF {
			logAgent(stream.Context(), "EOF (close) from", nil)
			return nil
		}
		if err != nil {
			logAgent(stream.Context(), "Connection error from", err)
			return err
		}
		logAgent(stream.Context(), "Update from", cl)
		s.lock.Lock()
		s.updateCluster(&cluster{cl, stream})
		s.lock.Unlock()
	}
	return nil
}

func (s *wardenServer) lookupRequest(req *warden.ClusterRequest) (*cluster, bool) {
	// Note: callers must hold s.lock
	rId := req.RequestId
	k, ok := s.requests[rId]
	if ok {
		ad, ok := s.clusters[k]
		if !ok {
			// this shouldn't happen, but we'll remove the stale request id to cluster id mapping
			delete(s.requests, rId)
			return nil, false
		}
		return &ad, true
	}
	return nil, false
}

func (s *wardenServer) assignRequest(req *warden.ClusterRequest) (*cluster, bool) {
	// Note: callers must hold s.lock
	for _, c := range s.clusters {
		if req.ClusterType != "" && req.ClusterType != c.ad.ClusterType {
			continue
		}
		if req.ClusterId != "" && req.ClusterId != c.ad.ClusterId {
			continue
		}
		// find the first one that is available
		if c.ad.State == warden.ClusterAdvertisement_AVAILABLE {
			k := key{c.ad.ClusterId, c.ad.ClusterType}
			// Update the request with cluster info
			req.ClusterType = c.ad.ClusterType
			req.ClusterId = c.ad.ClusterId

			// Mark the cluster as reserved internally so that it is not reassigned
			c.ad.State = warden.ClusterAdvertisement_RESERVED
			c.ad.RequestId = req.RequestId
			s.clusters[k] = c
			s.requests[req.RequestId] = k
			fmt.Println("Assigning cluster:", c.ad)
			return &c, true
		}
	}
	return nil, false
}

func (s *wardenServer) waitForReady(cl *cluster) (wait chan *warden.ClusterAdvertisement) {
	// Note: callers must hold s.lock
	// We allocate a buffered channel, so that we will not block if the cluster is already ready
	wait = make(chan *warden.ClusterAdvertisement, 1)
	if cl.ad.State == warden.ClusterAdvertisement_READY {
		// cluster is already ready, return immediately
		wait <- cl.ad
		return
	}
	// register this channel with the server to listen for updates
	k := key{cl.ad.ClusterId, cl.ad.ClusterType}
	l, ok := s.waiters[k]
	if !ok {
		l = []chan *warden.ClusterAdvertisement{wait}
	} else {
		l = append(l, wait)
	}
	s.waiters[k] = l
	return wait
}

func (s *wardenServer) processRequest(req *warden.ClusterRequest) (chan *warden.ClusterAdvertisement, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	var cl *cluster
	var found bool

	// Check to see if we have already reserved a cluster for this request
	cl, found = s.lookupRequest(req)

	if !found && req.Type == warden.ClusterRequest_RESERVE {
		// Assign the request to an available cluster
		cl, found = s.assignRequest(req)
	}
	if !found {
		return nil, fmt.Errorf("No available clusters for req %s", req.RequestId)
	}

	// Forward the request to the agent, except for status requests
	if req.Type != warden.ClusterRequest_STATUS {
		err := cl.agent.Send(req)
		if err != nil {
			return nil, err
		} else {
			logAgent(cl.agent.Context(), "Sending request to", req)
		}
	}

	// Wait for the cluster to become ready
	return s.waitForReady(cl), nil
}

func (s *wardenServer) returnCluster(cl *cluster) {
	// Note: callers must hold s.lock
	// Mark the cluster as unavailable internally in case a client asks
	k := keyFromCluster(cl)
	cl.ad.State = warden.ClusterAdvertisement_UNAVAILABLE
	s.clusters[k] = *cl

	// Build minimal request based on cluster advertisement
	req := warden.ClusterRequest{
		ClusterId:   cl.ad.ClusterId,
		ClusterType: cl.ad.ClusterType,
		RequestId:   cl.ad.RequestId,
		Type:        warden.ClusterRequest_RETURN,
	}
	logAgent(cl.agent.Context(), "Sending internal return request to", req)
	cl.agent.Send(&req)
}

func (s *wardenServer) cleanupStaleClusters() {
	for {
		s.lock.Lock()
		for _, cl := range s.clusters {
			switch cl.ad.State {
			case warden.ClusterAdvertisement_RESERVED:
				fallthrough
			case warden.ClusterAdvertisement_READY:
				info := cl.ad.ReservationInfo
				if info != nil {
					if info.Duration < 0 {
						// Reservation does not expire
						continue
					}
					end := time.Unix(info.ReservationStartTime, 0)
					end = end.Add(time.Duration(info.Duration) * time.Minute)
					if end.Before(time.Now()) {
						fmt.Println("Reservation expired:", cl.ad)
						s.returnCluster(&cl)
						continue
					} else {
						fmt.Println("Time remaining in seconds", cl.ad.ClusterId, cl.ad.ClusterType, end.Sub(time.Now()))

					}
				}
			}
		}
		s.lock.Unlock()
		time.Sleep(20 * time.Second)
	}
}

func newServer() *wardenServer {
	s := new(wardenServer)
	s.clusters = make(map[key]cluster)
	s.requests = make(map[string]key)
	s.clients = make(map[warden.ClusterClientService_ServerClustersServer]bool)
	s.agents = make(map[warden.ClusterAgentService_AgentClustersServer]bool)
	s.waiters = make(map[key][]chan *warden.ClusterAdvertisement)
	return s
}

func main() {
	lis, err := net.Listen("tcp", ":1234")
	if err != nil {
		grpclog.Fatalf("failed to listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	s := newServer()
	go s.cleanupStaleClusters()
	warden.RegisterClusterClientServiceServer(grpcServer, s)
	warden.RegisterClusterAgentServiceServer(grpcServer, s)
	fmt.Println("starting to serve...")
	grpcServer.Serve(lis)
}
