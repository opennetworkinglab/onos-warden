package main

import (
	"github.com/opennetworkinglab/onos-warden/agent"
	"github.com/opennetworkinglab/onos-warden/warden"
	"math/rand"
	"time"
)

// Setup a few canned cells
// send periodic advertisements

var cannedCells = []warden.ClusterAdvertisement{
	{ClusterId: "alpha", ClusterType: "lxc"},
	{ClusterId: "bravo", ClusterType: "lxc"},
	{ClusterId: "charlie", ClusterType: "lxc"},
}

type lxcClient struct {
	client agent.WardenClient
	// TODO information about the local bare-metal, i.e. name, cell names, ip-ranges, etc.
}

// Creates a new LXC agent worker
func NewAgentWorker() (agent.Worker, error) {
	var c lxcClient

	go func() {
		for {
			time.Sleep(5 * time.Second)
			ad := cannedCells[rand.Intn(len(cannedCells))]
			c.client.PublishUpdate(&ad)
		}
	}()

	return &c, nil
}

func (c *lxcClient) Bind(client agent.WardenClient) {
	c.client = client
}

func (c *lxcClient) Teardown() {
	//TODO
}

func (c *lxcClient) Handle(req *warden.ClusterRequest) {
	//TODO
}

func (c *lxcClient) updateInstances() {
}

// Runs Warden agent using LXC worker on internal bare-metal machines.
func main() {
	agent.Run(NewAgentWorker())
}
