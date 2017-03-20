package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/opennetworkinglab/onos-warden/warden"
	"net"
	"strconv"
	"time"
)

func (c *ec2Client) makeSpotRequest(cl *cluster) error {
	if cl.InstanceId != "" {
		return errors.New("Instance already exists for this cluster")
	}

	dm := ec2.BlockDeviceMapping{
		DeviceName: aws.String("/dev/sda1"),
		Ebs: &ec2.EbsBlockDevice{
			DeleteOnTermination: aws.Bool(true),
			Encrypted:           aws.Bool(false),
			VolumeSize:          aws.Int64(16),
			VolumeType:          aws.String("gp2"),
		},
	}

	r := ec2.RequestSpotInstancesInput{
		LaunchSpecification: &ec2.RequestSpotLaunchSpecification{
			ImageId:             aws.String(InstanceImageId),
			InstanceType:        aws.String(InstanceType),
			KeyName:             aws.String(KeyName),
			SecurityGroupIds:    aws.StringSlice([]string{"all open"}),
			BlockDeviceMappings: []*ec2.BlockDeviceMapping{&dm},
		},
		SpotPrice: aws.String(MaxPrice),
	}

	out, err := c.svc.RequestSpotInstances(&r)
	if err != nil {
		return fmt.Errorf("Could not complete request: %v\n%v", r, err)
	}

	ids := make([]*string, 1)
	for _, r := range out.SpotInstanceRequests {
		ids = append(ids, r.SpotInstanceRequestId)
	}
	fmt.Print("Wait for reservation...")
	for { // Wait for reservation to be fulfilled
		time.Sleep(startupPollingInterval)
		out, err := c.svc.DescribeSpotInstanceRequests(&ec2.DescribeSpotInstanceRequestsInput{
			SpotInstanceRequestIds: ids,
		})
		if err != nil {
			fmt.Println(err)
			continue
		}
		for _, r := range out.SpotInstanceRequests {
			if r.InstanceId != nil && *r.InstanceId != "" {
				fmt.Println(*r.InstanceId)
				cl.InstanceId = *r.InstanceId
				break
			}
		}
		if cl.InstanceId != "" {
			break
		}
		fmt.Print(".")
	}
	//TODO: OR consider...
	//c.svc.WaitUntilSpotInstanceRequestFulfilled(&ec2.DescribeSpotInstanceRequestsInput{
	//	SpotInstanceRequestIds: ids,
	//})

	fmt.Print("Wait for start...")
	for { // Wait for instance to start
		out, err := c.svc.DescribeInstances(&ec2.DescribeInstancesInput{
			InstanceIds: aws.StringSlice([]string{cl.InstanceId}),
		})
		if err != nil {
			fmt.Println(err)
			continue
		}

		// Collect the cluster updates
		var targetCl *cluster
		for _, res := range out.Reservations {
			for _, inst := range res.Instances {
				cl, err := clusterFromInstance(inst)
				if err == nil {
					targetCl = &cl
				}
			}
		}

		if targetCl != nil && targetCl.InstanceStarted {
			// Copy the node IP over from the newly created instance
			cl.HeadNodeIP = targetCl.HeadNodeIP
			fmt.Println(cl)
			break
		}
		time.Sleep(startupPollingInterval)
		fmt.Print(".")
	}
	//TODO: OR consider...
	//c.svc.WaitUntilInstanceRunning(&ec2.DescribeInstancesInput{
	//	InstanceIds: aws.StringSlice([]string{id}),
	//})
	return nil
}

func tag(k, v string) *ec2.Tag {
	return &ec2.Tag{Key: &k, Value: &v}
}

func (c *ec2Client) tagInstance(inst string, cl *cluster) error {
	id, reqId := cl.ClusterId, cl.RequestId
	size := cl.Size
	tags := make([]*ec2.Tag, 0)
	tags = append(tags,
		tag("Cell-Id", id),
		tag("Name", InstanceName),
		tag("Cell-Size", strconv.FormatUint(uint64(size), 10)))

	if reqId != "" && cl.ReservationInfo != nil {
		user := cl.ReservationInfo.UserName
		start := cl.ReservationInfo.ReservationStartTime
		duration := cl.ReservationInfo.Duration
		tags = append(tags,
			tag("Cell-Request-Id", reqId),
			tag("Cell-Start", strconv.FormatUint(uint64(start), 10)),
			tag("Cell-Duration", strconv.FormatInt(int64(duration), 10)),
			tag("Cell-User", user),
			tag("Cell-Provisioned", strconv.FormatBool(cl.State == warden.ClusterAdvertisement_READY)))
	} else {
		tags = append(tags,
			tag("Cell-Request-Id", ""),
			tag("Cell-Start", ""),
			tag("Cell-Duration", ""),
			tag("Cell-User", ""),
			tag("Cell-Provisioned", ""))
	}

	_, err := c.svc.CreateTags(&ec2.CreateTagsInput{
		Resources: aws.StringSlice([]string{inst}),
		Tags:      tags,
	})
	return err
}

func (c *ec2Client) terminateInstance(cl cluster) {
	_, err := c.svc.TerminateInstances(&ec2.TerminateInstancesInput{
		InstanceIds: aws.StringSlice([]string{cl.InstanceId}),
	})
	if err != nil {
		fmt.Println(err)
	}

	fmt.Printf("Terminating %s (%s)\n", cl.ClusterId, cl.InstanceId)
	c.mux.Lock()
	defer c.mux.Unlock()
	c.addOrUpdate(emptyCluster(cl.ClusterId))
}

func (c *ec2Client) updateInstances() error {
	// Request instances from EC2 that are tagged with "Cell-Id"
	filter := ec2.Filter{
		Name:   aws.String("tag-key"),
		Values: aws.StringSlice([]string{"Cell-Id"}),
	}
	in := ec2.DescribeInstancesInput{Filters: []*ec2.Filter{&filter}}
	resp, err := c.svc.DescribeInstances(&in)
	if err != nil {
		fmt.Println("Failed to get instances", err)
		return err
	}

	c.mux.Lock()
	defer c.mux.Unlock()

	update := make(map[string]bool)
	for k := range c.clusters {
		update[k] = false
	}

	// Collect the cluster updates
	for _, res := range resp.Reservations {
		for _, inst := range res.Instances {
			cl, err := clusterFromInstance(inst)
			update[cl.ClusterId] = true
			if err == nil {
				if shouldShutdown(&cl) {
					go c.terminateInstance(cl)
				} else {
					c.addOrUpdate(cl)
				}
			}
		}
	}

	// Remove clusters that are missing from EC2
	for k, updated := range update {
		if !updated {
			c.addOrUpdate(emptyCluster(k))
		}
	}
	return nil
}

func clusterFromInstance(inst *ec2.Instance) (c cluster, err error) {
	c = cluster{
		ClusterAdvertisement: warden.ClusterAdvertisement{
			ClusterType:     ClusterType,
			ReservationInfo: &warden.ClusterAdvertisement_ReservationInfo{},
		},
		InstanceId:   *inst.InstanceId,
		InstanceType: *inst.InstanceType,
		LaunchTime:   *inst.LaunchTime,
	}
	if inst.PublicIpAddress != nil {
		c.HeadNodeIP = *inst.PublicIpAddress
	}
	switch *inst.State.Code {
	case 16: // "running"
		c.State = warden.ClusterAdvertisement_AVAILABLE
		c.InstanceStarted = true
	case 48: // "terminated
		err = errors.New("terminated")
		return
	default:
		c.State = warden.ClusterAdvertisement_UNAVAILABLE
		c.InstanceStarted = false
	}
	provisioned := false
	for _, t := range inst.Tags {
		k, v := *t.Key, *t.Value
		if v == "" {
			continue
		}
		switch k {
		case "Cell-Id":
			c.ClusterId = v
		case "Cell-Request-Id":
			c.RequestId = v
			if v != "" && c.State == warden.ClusterAdvertisement_AVAILABLE {
				c.State = warden.ClusterAdvertisement_RESERVED
			}
		case "Cell-Size":
			i, err := strconv.ParseUint(v, 10, 32)
			if err == nil {
				c.Nodes = make([]*warden.ClusterAdvertisement_ClusterNode, i+1)
				c.Size = uint32(i)
			} else {
				fmt.Println("Failed to parse Cell-Size", v, err)
			}
		case "Cell-Start":
			i, err := strconv.ParseInt(v, 10, 64)
			if err == nil {
				c.ReservationInfo.ReservationStartTime = i
			} else {
				fmt.Println("Failed to parse Cell-Start", v, err)
			}
		case "Cell-Duration":
			i, err := strconv.ParseInt(v, 10, 32)
			if err == nil {
				c.ReservationInfo.Duration = int32(i)
			} else {
				fmt.Println("Failed to parse Cell-Duration", v, err)
			}
		case "Cell-User":
			c.ReservationInfo.UserName = v
		case "Cell-Provisioned":
			provisioned = v == "true"
		}
	}
	if provisioned && c.State == warden.ClusterAdvertisement_RESERVED {
		c.State = warden.ClusterAdvertisement_READY
	}
	if cap(c.Nodes) > 0 {
		for i := range c.Nodes {
			var cn warden.ClusterAdvertisement_ClusterNode
			// Note: index 0 is the network node
			cn.Id = uint32(i)

			ip := make(net.IP, 4)
			binary.BigEndian.PutUint32(ip, IpBase+uint32(i))
			cn.Ip = ip.String()
			c.Nodes[i] = &cn
		}
	}
	return
}
