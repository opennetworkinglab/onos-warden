package agent

import (
	"fmt"
	"github.com/opennetworkinglab/onos-warden/util"
	"github.com/opennetworkinglab/onos-warden/warden"
)

type Worker interface {
	Bind(client WardenClient)
	Handle(req *warden.ClusterRequest)
	Start()
	Teardown()
}

type agent struct {
	grpc   WardenClient
	worker Worker
}

// TODO: also pass in the Warden server IP address
func Run(worker Worker, err error) {
	a := agent{}
	address := "127.0.0.1:1234"

	a.worker = worker
	if err != nil {
		panic(err)
	} else {
		fmt.Println("Started agent worker")
	}
	defer a.worker.Teardown()

	a.grpc, err = NewWardenClient(address, a.worker, nil)
	if err != nil {
		panic(err)
	} else {
		worker.Bind(a.grpc)
		fmt.Println("Started gRPC warden client")
	}
	defer a.grpc.Teardown()

	worker.Start()

	util.WaitForInterrupt()

	fmt.Println("Exiting...")
	//TODO consider withdrawing all clusters before teardown?
}
