package main

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/opennetworkinglab/onos-warden/agent"
	"github.com/opennetworkinglab/onos-warden/warden"
	"strconv"
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
		for i, _ := range slots {
			c.client.PublishUpdate(&warden.ClusterAdvertisement{
				ClusterId: strconv.Itoa(i),
				ClusterType: "ec2",
			})
		}
		c.updateInstances()
		//for {
		//	c.updateInstances()
		//	c.client.PublishUpdate(&warden.ClusterAdvertisement{})
		//	time.Sleep(5 * time.Second)
		//}
	}()
}

func (c *ec2Client) Teardown() {
	//TODO
}

func (c *ec2Client) Handle(req *warden.ClusterRequest) {
	//TODO
}

func getReservation(i *ec2.Instance) (warden.ClusterAdvertisement, error) {
	return nil, nil
}

func (c *ec2Client) updateInstances() {
	// Call the DescribeInstances Operation
	fmt.Println("starting...")

	filter := ec2.Filter{
		Name: aws.String("tag-key"),
		Values: aws.StringSlice([]string{"Cell-Id"}),
	}
	in := ec2.DescribeInstancesInput{Filters: []*ec2.Filter{&filter}}
	resp, err := c.svc.DescribeInstances(&in)
	if err != nil {
		panic(err)
	}

	// resp has all of the response data, pull out instance IDs:
	fmt.Println("> Number of reservation sets: ", len(resp.Reservations))
	for idx, res := range resp.Reservations {
		fmt.Println("  > Number of instances: ", len(res.Instances))
		for _, inst := range resp.Reservations[idx].Instances {
			fmt.Println("    - Instance ID: ", *inst.InstanceId)
			fmt.Println(inst.Tags)
		}
	}
//-------------------------------------
//	params := &ec2.DescribeInstancesInput{
//		Filters: []*ec2.Filter{
//			//{
//			//	Name:   aws.String("tag:Environment"),
//			//	Values: []*string{aws.String("prod")},
//			//},
//			{
//				Name:   aws.String("instance-state-name"),
//				Values: []*string{aws.String("running")},
//			},
//		},
//	}
//	resp, err := c.svc.DescribeInstances(params)
//	if err != nil {
//		fmt.Println("there was an error listing instances in", err.Error())
//		log.Fatal(err.Error())
//	}
//
//	for idx, res := range resp.Reservations {
//		fmt.Println("  > Reservation Id", *res.ReservationId, " Num Instances: ", len(res.Instances))
//		for _, inst := range resp.Reservations[idx].Instances {
//			fmt.Println("    - Instance ID: ", *inst.InstanceId)
//		}
//	}
}

// name of cell
// reservation info

var slots []struct{}
func main() {
	slots = make([]struct{}, 10)
	agent.Run(NewEC2Client(DefaultAwsRegion))
}
