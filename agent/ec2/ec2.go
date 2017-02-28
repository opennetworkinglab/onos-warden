package main

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/opennetworkinglab/onos-warden/agent"
	"github.com/opennetworkinglab/onos-warden/warden"
	"time"
)

const DefaultAwsRegion string = "us-west-1"

type ec2Client struct {
	svc *ec2.EC2
	client agent.WardenClient
}

func NewEC2Client(region string) (agent.Worker, error) {
	var c ec2Client

	sess, err := session.NewSession()
	if err != nil {
		return nil, err
	}

	c.svc = ec2.New(sess, aws.NewConfig().WithRegion(region))


	return &c, err
}

func (c *ec2Client) Bind(client agent.WardenClient) {
	c.client = client
}


func (c *ec2Client) Start() {
	go func() {
		for {
			c.updateInstances()
			c.client.PublishUpdate(&warden.ClusterAdvertisement{})
			time.Sleep(5 * time.Second)
		}
	}()
}

func (c *ec2Client) Teardown() {
	//TODO
}

func (c *ec2Client) Handle(req *warden.ClusterRequest) {
	//TODO
}

func (c *ec2Client) updateInstances() {
	// Call the DescribeInstances Operation
	resp, err := c.svc.DescribeInstances(nil)
	if err != nil {
		panic(err)
	}

	// resp has all of the response data, pull out instance IDs:
	fmt.Println("> Number of reservation sets: ", len(resp.Reservations))
	for idx, res := range resp.Reservations {
		fmt.Println("  > Number of instances: ", len(res.Instances))
		for _, inst := range resp.Reservations[idx].Instances {
			fmt.Println("    - Instance ID: ", *inst.InstanceId)
		}
	}
}

func main() {
	agent.Run(NewEC2Client(DefaultAwsRegion))
}
