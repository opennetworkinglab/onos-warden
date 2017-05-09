package main

import (
	"encoding/binary"
	"fmt"
	"github.com/opennetworkinglab/onos-warden/agent"
	"github.com/opennetworkinglab/onos-warden/warden"
	"net"
	"reflect"
	"sync"
	"time"
)

const (
	numCells    = 3
	clusterType = "dummy"
)

type client struct {
	grpc     agent.WardenClient
	cells    map[string]warden.ClusterAdvertisement
	requests map[string]string
	mux      sync.Mutex
}

func NewAgentWorker() (agent.Worker, error) {
	var c client
	c.cells = make(map[string]warden.ClusterAdvertisement)
	c.requests = make(map[string]string)
	return &c, nil
}

func (c *client) Start() {
	for i := 0; i < numCells; i++ {
		c.updateRequest(&warden.ClusterAdvertisement{
			ClusterId:   agent.GetWord(string(rune('a' + i))),
			ClusterType: clusterType,
			State:       warden.ClusterAdvertisement_AVAILABLE,
			HeadNodeIP:  "1.2.3.4",
		})
	}
}

func (c *client) Bind(client agent.WardenClient) {
	c.grpc = client
}

func (c *client) Teardown() {
	c.mux.Lock()
	defer c.mux.Unlock()

	// Don't worry about the map, we are going away
	for _, ad := range c.cells {
		ad.State = warden.ClusterAdvertisement_UNAVAILABLE
		c.grpc.PublishUpdate(&ad)
	}
}

func (c *client) Handle(req *warden.ClusterRequest) {
	if req.ClusterType != "" && req.ClusterType != clusterType {
		fmt.Println("Cannot handle cluster type", req.ClusterType)
		return
	}
	ad, ok := c.getRequest(req.ClusterId, req.RequestId)
	if !ok {
		fmt.Println("Cannot find cluster id", req.ClusterId)
		return
	}
	if ad.RequestId != "" && ad.RequestId != req.RequestId {
		fmt.Printf("Requested id %s does not match exisiting id %s\n", req.RequestId, ad.RequestId)
		return
	}

	switch req.Type {
	case warden.ClusterRequest_RESERVE:
		if ad.ReservationInfo != nil {
			fmt.Println("Could not reserve cell", ad, req)
			return
		}
		ad.State = warden.ClusterAdvertisement_RESERVED
		ad.RequestId = req.RequestId
		if req.Spec == nil {
			fmt.Println("req spec is nil", req)
			return
		}
		ad.ReservationInfo = &warden.ClusterAdvertisement_ReservationInfo{
			UserName:             req.Spec.UserName,
			Duration:             req.Duration,
			ReservationStartTime: time.Now().Unix(),
		}
		ad.Nodes = make([]*warden.ClusterAdvertisement_ClusterNode, req.Spec.ControllerNodes)
		for i := range ad.Nodes {
			id := uint32(i + 1)
			ip := make(net.IP, 4)
			binary.BigEndian.PutUint32(ip, id)
			ad.Nodes[i] = &warden.ClusterAdvertisement_ClusterNode{
				Id: id,
				Ip: ip.String(),
			}
		}
		go func(a warden.ClusterAdvertisement) {
			// update a copy after 5 seconds to simulate provisioning
			time.Sleep(5 * time.Second)
			a.State = warden.ClusterAdvertisement_READY
			c.updateRequest(&a)
		}(ad)
	case warden.ClusterRequest_EXTEND:
		if ad.ReservationInfo == nil {
			fmt.Println("Could not extend reservation; reservation info missing", req)
			return
		}
		// Update the duration field
		start := time.Unix(int64(ad.ReservationInfo.ReservationStartTime), int64(0))
		past := time.Since(start)
		newDuration := int32(float64(req.Duration) + past.Minutes())
		ad.ReservationInfo.Duration = newDuration
	case warden.ClusterRequest_RETURN:
		ad.State = warden.ClusterAdvertisement_AVAILABLE
		ad.RequestId = ""
		ad.Nodes = nil
		ad.ReservationInfo = nil
	}
	fmt.Println("Updating", ad)
	c.updateRequest(&ad)
}

func (c *client) getRequest(cId, rId string) (w warden.ClusterAdvertisement, retOk bool) {
	retOk = false

	c.mux.Lock()
	defer c.mux.Unlock()

	if rId != "" {
		v, ok := c.requests[rId]
		if ok {
			if cId != "" && cId != v {
				// request is present; cId does not match request's cluster id
				return
			} else {
				// uses the request's existing cluster id
				cId = v
			}
		}
	}

	if cId != "" {
		ad, ok := c.cells[cId]
		if ok {
			return ad, true
		}
	} else {
		// reserve an available cluster
		for _, v := range c.cells {
			if v.RequestId != "" && rId != v.RequestId {
				continue
			}
			if v.State == warden.ClusterAdvertisement_AVAILABLE {
				return v, true
			}
		}
	}
	return
}

func (c *client) updateRequest(ad *warden.ClusterAdvertisement) {
	c.mux.Lock()
	defer c.mux.Unlock()

	id := ad.ClusterId
	existing, ok := c.cells[id]
	if !ok || !reflect.DeepEqual(existing, ad) {
		c.cells[id] = *ad
		c.grpc.PublishUpdate(ad)
	}
	if ad.RequestId != "" {
		// request id was added
		c.requests[ad.RequestId] = id
	} else if existing.RequestId != "" {
		// request id was dropped
		delete(c.requests, ad.RequestId)
	}
}

func main() {
	agent.Run(NewAgentWorker())
}
