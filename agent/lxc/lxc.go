package main

import (
	"github.com/opennetworkinglab/onos-warden/agent"
	"github.com/opennetworkinglab/onos-warden/warden"
)

type lxcClient struct {
	// TODO information about the local bare-metal, i.e. name, cell names, ip-ranges, etc.
}

// Creates a new LXC agent worker
func NewAgentWorker() (agent.Worker, error) {
	var c lxcClient

	return &c, nil
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
