package main

import (
	"fmt"
	"github.com/opennetworkinglab/onos-warden/util"
	"github.com/opennetworkinglab/onos-warden/warden"
)

type agent struct {
	ec2  EC2Client
	grpc WardenClient
}

func (a *agent) Handle(req *warden.ClusterRequest) {
	//TODO implement logic to forward request to EC2
}

func main() {
	a := agent{}
	var err error

	a.ec2, err = NewEC2Client(DefaultAwsRegion)
	if err != nil {
		panic(err)
	} else {
		fmt.Println("Started EC2 client.")
	}
	defer a.ec2.Teardown()

	a.grpc, err = NewWardenClient("127.0.0.1:1234", &a, nil)
	if err != nil {
		panic(err)
	} else {
		fmt.Println("Started gRPC Warden client.")
	}
	defer a.grpc.Teardown()

	util.WaitForInterrupt()

	fmt.Println("Exiting...")
	//TODO consider withdrawing all clusters before teardown?
}
