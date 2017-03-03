package main

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/opennetworkinglab/onos-warden/agent"
	"github.com/opennetworkinglab/onos-warden/warden"
	"strconv"
	"net"
	"encoding/binary"
	"time"
	"sync"
	"errors"
	"reflect"
)

const (
	DefaultAwsRegion string = "us-west-1"
	ClusterType = "ec2"
	InstanceName = "warden-cell"
)
var IpBase = binary.BigEndian.Uint32(net.ParseIP("10.0.0.0")[12:16])

type ec2Client struct {
	svc      *ec2.EC2
	client   agent.WardenClient
	clusters map[string]cluster
	limit    int
	mux      sync.Mutex
}

func NewEC2Client(region string, limit int) (agent.Worker, error) {
	var c ec2Client

	sess, err := session.NewSession()
	if err != nil {
		return nil, err
	}

	c.svc = ec2.New(sess, aws.NewConfig().WithRegion(region))
	c.clusters = make(map[string]cluster)
	c.limit = limit

	return &c, err
}

func (c *ec2Client) Bind(client agent.WardenClient) {
	c.client = client
}

func (c *ec2Client) Start() {
	err := c.updateInstances()
	if err != nil {
		fmt.Println("Failed to populate initial clusters")
		panic(err)
	}

	// Add placeholder clusters as needed, up to the limit
	for i := len(c.clusters); i < c.limit; i++ {
		c.addOrUpdate(getPlaceholderCluster(i))
	}

	// Start goroutine to periodically update clusters
	go func() {
		for {
			time.Sleep(5 * time.Second)
			c.updateInstances()
		}
	}()
}

func (c *ec2Client) Teardown() {
	//TODO
	fmt.Println("teardown...")
}

func (c *ec2Client) Handle(req *warden.ClusterRequest) {
	switch req.Type {
	case warden.ClusterRequest_RESERVE:
		cl, err := c.reserveCluster(req)
		if err != nil {
			fmt.Println("Unable process reservation", req, err)
			return
		}
		c.provisionCluster(cl, req.Spec.UserKey)
	case warden.ClusterRequest_EXTEND:
		//TODO
	case warden.ClusterRequest_RETURN:
		//TODO
	default:
		fmt.Println("Unsupported request: %+v\n", req)
	}
}

type cluster struct{
	warden.ClusterAdvertisement
	Size uint32
	InstanceId string
	InstanceType string
	InstanceStarted bool
	LaunchTime time.Time
	Provisioned bool
}

func getCluster(inst *ec2.Instance) (c cluster, err error) {
	c = cluster {
		ClusterAdvertisement: warden.ClusterAdvertisement{
			ClusterType: ClusterType,
			ReservationInfo: &warden.ClusterAdvertisement_ReservationInfo{},
		},
		InstanceId: *inst.InstanceId,
		InstanceType: *inst.InstanceType,
		LaunchTime: *inst.LaunchTime,
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
	for _, t := range inst.Tags {
		switch k, v := *t.Key, *t.Value; k {
		case "Cell-Id":
			c.ClusterId = v
		case "Cell-Request-Id":
			c.RequestId = v
			if c.State == warden.ClusterAdvertisement_AVAILABLE {
				c.State = warden.ClusterAdvertisement_RESERVED
			}
		case "Cell-Size":
			i, err := strconv.ParseUint(v, 10, 32)
			if err == nil {
				c.Nodes = make([]*warden.ClusterAdvertisement_ClusterNode, i)
				c.Size = uint32(i)
			} else {
				fmt.Println("Failed to parse Cell-Size", v, err)
			}
		case "Cell-Start":
			i, err := strconv.ParseUint(v, 10, 32)
			if err == nil {
				c.ReservationInfo.ReservationStartTime = uint32(i)
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
			c.Provisioned = v == "true"
		}
	}
	if !c.Provisioned && c.State == warden.ClusterAdvertisement_RESERVED {
		// Reserved, but not yet provisioned, instances are unavailable
		c.State = warden.ClusterAdvertisement_UNAVAILABLE
	}
	if cap(c.Nodes) > 0 {
		ipNum := IpBase
		for i := range c.Nodes {
			ipNum++
			ip := make(net.IP, 4)
			binary.BigEndian.PutUint32(ip, ipNum)
			c.Nodes[i] = &warden.ClusterAdvertisement_ClusterNode{
				Id: uint32(i + 1),
				Ip: ip.String(),
			}
		}
	}
	return
}

func emptyCluster(id string) cluster {
	return cluster {
		ClusterAdvertisement: warden.ClusterAdvertisement{
			ClusterType: ClusterType,
			State: warden.ClusterAdvertisement_AVAILABLE,
			ClusterId: id,
		},
	}
}

func getPlaceholderCluster(i int) cluster {
	i = i % 26
	name := agent.GetWord(string(rune('a' + i)))
	return emptyCluster(name)
}

func tag(k, v string) *ec2.Tag {
	return &ec2.Tag{Key: &k, Value: &v}
}

func (c *ec2Client) tagInstance(inst string, cl *cluster) error {
	id, reqId := cl.ClusterId, cl.RequestId
	user := cl.ReservationInfo.UserName
	size, start := cl.Size, cl.ReservationInfo.ReservationStartTime
	duration := cl.ReservationInfo.Duration

	tags := make([]*ec2.Tag, 0)
	tags = append(tags, tag("Cell-Id", id), tag("Name", InstanceName))

	fmt.Printf("%+v\n", *cl)
	if reqId != "" {
		tags = append(tags,
			tag("Cell-Request-Id", reqId),
			tag("Cell-Size", strconv.FormatUint(uint64(size), 10)),
			tag("Cell-Start", strconv.FormatUint(uint64(start), 10)),
			tag("Cell-Duration", strconv.FormatInt(int64(duration), 10)),
			tag("Cell-User", user),
			tag("Cell-Provisioned", strconv.FormatBool(cl.Provisioned)))
	} else {
		tags = append(tags,
				tag("Cell-Request-Id", ""),
				tag("Cell-Size", ""),
				tag("Cell-Start", ""),
				tag("Cell-Duration", ""),
				tag("Cell-User", ""),
				tag("Cell-Provisioned", ""))
	}

	fmt.Println(tags)
	_, err := c.svc.CreateTags(&ec2.CreateTagsInput{
		Resources: aws.StringSlice([]string{inst}),
		Tags: tags,
	})
	return err
}

func equal(a, b *warden.ClusterAdvertisement) bool {
	if a == b {
		return true
	}
	if a.ClusterId != b.ClusterId {
		return false
	}
	if a.ClusterType != b.ClusterType {
		return false
	}
	if a.State != b.State {
		return false
	}
	if a.RequestId != b.RequestId {
		return false
	}
	if a.HeadNodeIP != b.HeadNodeIP {
		return false
	}
	if len(a.Nodes) != len(b.Nodes) {
		return false
	}
	for i := range a.Nodes {
		nA, nB := a.Nodes[i], b.Nodes[i]
		if nA == nB {
			continue
		}
		if nA == nil {
			return false
		}
		if nA.Id != nB.Id  {
			return false
		}
		if nA.Ip != nB.Ip {
			return false
		}
	}

	if a.ReservationInfo != b.ReservationInfo {
		if a.ReservationInfo == nil {
			return false
		}
		if a.ReservationInfo.UserName != b.ReservationInfo.UserName {
			return false
		}
		if a.ReservationInfo.Duration != b.ReservationInfo.Duration {
			return false
		}
		if a.ReservationInfo.ReservationStartTime != b.ReservationInfo.ReservationStartTime {
			return false
		}
	}

	return true
}

func (c *ec2Client) addOrUpdate(cl cluster) {
	c.mux.Lock()
	defer c.mux.Unlock()

	id := cl.ClusterId
	old, ok := c.clusters[id]
	c.clusters[id] = cl
	//TODO consider !reflect.DeepEqual() vs equal()
//	if !ok || !equal(&cl.ClusterAdvertisement, &old.ClusterAdvertisement) {
	if !ok || !reflect.DeepEqual(cl.ClusterAdvertisement, old.ClusterAdvertisement) {
		fmt.Printf("Updating: %+v\n", cl)
		c.client.PublishUpdate(&cl.ClusterAdvertisement)
	} else {
		fmt.Println("new or equal", id)
	}
}

func (c *ec2Client) get(id string) (cluster, bool) {
	c.mux.Lock()
	defer c.mux.Unlock()

	cl, ok := c.clusters[id]
	return cl, ok
}

func (c *ec2Client) reserveCluster(req *warden.ClusterRequest) (*cluster, error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	if req.ClusterType != ClusterType {
		return nil, fmt.Errorf("Requested cluster type %s is not %s", req.ClusterType, ClusterType)
	}

	id := req.ClusterId
	var cl cluster
	if id == "" {
		for _, v := range c.clusters {
			if v.State == warden.ClusterAdvertisement_AVAILABLE {
				cl = v
				break
			}
		}
		if cl.ClusterId == "" {
			return nil, errors.New("No available clusters")
		}
	} else {
		v, ok := c.clusters[id]
		if !ok {
			return nil, fmt.Errorf("Cluster %s not found", id)
		} else if v.State != warden.ClusterAdvertisement_AVAILABLE {
			return nil, fmt.Errorf("Cluster %s not available", id)
		}
		cl = v
	}

	cl.State = warden.ClusterAdvertisement_RESERVED
	cl.Size = req.Spec.ControllerNodes
	cl.RequestId = req.RequestId
	cl.ReservationInfo = &warden.ClusterAdvertisement_ReservationInfo{
		UserName: req.Spec.UserName,
		Duration: req.Duration,
		ReservationStartTime: uint32(time.Now().Unix()),
	}
	cl.Provisioned = false

	// Update the cluster to include the reservation in the map,
	// but don't send update until it is provisioned
	c.clusters[cl.ClusterId] = cl
	return &cl, nil
}

func (c *ec2Client) requestInstance (cl *cluster, wait chan string) {
	dm := ec2.BlockDeviceMapping{
		DeviceName: aws.String("/dev/sda1"),
		Ebs: &ec2.EbsBlockDevice{
			DeleteOnTermination: aws.Bool(true),
			Encrypted: aws.Bool(false),
			VolumeSize: aws.Int64(16),
			VolumeType: aws.String("gp2"),
		},
	}

	r := ec2.RequestSpotInstancesInput{
		LaunchSpecification: &ec2.RequestSpotLaunchSpecification{
			ImageId: aws.String("ami-8d8c78c9"),
			InstanceType: aws.String("m3.medium"),
			KeyName: aws.String("onos-test"),
			SecurityGroupIds: aws.StringSlice([]string{"all open"}),
			BlockDeviceMappings: []*ec2.BlockDeviceMapping{&dm},
		},
		SpotPrice: aws.String("1"), //FIXME pick a price
	}

	out, err := c.svc.RequestSpotInstances(&r)
	if err != nil {
		fmt.Println("Could not complete request", r, err)
		return
	}
	ids := make([]*string, 1)
	for _, r := range out.SpotInstanceRequests {
		ids = append(ids, r.SpotInstanceRequestId)
	}
	go func() {
		var id string
		for { // Wait for reservation to be fulfilled
			fmt.Println("Wait for reservation...")
			time.Sleep(2 * time.Second)
			out, err := c.svc.DescribeSpotInstanceRequests(&ec2.DescribeSpotInstanceRequestsInput{
				SpotInstanceRequestIds: ids,
			})
			if err != nil {
				fmt.Println(err)
				continue
			}
			fmt.Println(out)
			for _, r := range out.SpotInstanceRequests {
				if r.InstanceId != nil && *r.InstanceId != "" {
					fmt.Println(*r.InstanceId)
					id = *r.InstanceId
					break
				}
			}
			if id != "" {
				err := c.tagInstance(id, cl)
				if err != nil {
					fmt.Println("Error tagging instance", id, err)
				}
				break
			}
		}
		for { // Wait for instance to start
			//TODO Busy waiting for now, but should be updated to listen to map events
			cl, ok := c.get(cl.ClusterId)
			fmt.Println("Wait for start...", cl, ok)
			if ok && cl.InstanceStarted {
				break
			}
			time.Sleep(1 * time.Second)
		}
		fmt.Println("started")
		wait <- id
		close(wait)
	}()
}

func (c *ec2Client) provisionCluster(cl *cluster, userKey string) {
	wait := make(chan string)
	if cl.InstanceId == "" {
		c.requestInstance(cl, wait)
	} else {
		//c.tagInstance()
		wait <- cl.InstanceId
		close(wait)
	}
	go func() {
		instanceId := <- wait
		fmt.Println("instance ready!!!", instanceId)

		cluster, ok := c.get(cl.ClusterId)
		if ok {
			cluster.Provisioned = true
			c.tagInstance(cluster.InstanceId, &cluster)
		} else {
			fmt.Println("failed to get", cl.ClusterId)
		}
		// wait for start
		// TODO set start time (== launch time?)
		// set by ec2: headNodeIp, Nodes, LaunchTime, InstanceId, InstanceType
	}()
}

func (c *ec2Client) updateInstances() error {
	// Request instances from EC2 that are tagged with "Cell-Id"
	filter := ec2.Filter{
		Name: aws.String("tag-key"),
		Values: aws.StringSlice([]string{"Cell-Id"}),
	}
	in := ec2.DescribeInstancesInput{Filters: []*ec2.Filter{&filter}}
	resp, err := c.svc.DescribeInstances(&in)
	if err != nil {
		fmt.Println("Failed to get instances", err)
		return err
	}

	// Update each cluster
	for _, res := range resp.Reservations {
		for _, inst := range res.Instances {
			cl, err := getCluster(inst)
			if err == nil {
				c.addOrUpdate(cl)
			}
			//TODO shutdown if AVAILABLE and nearing end of hour
		}
	}

	//TODO detect existing clusters that are missing in this update
	return nil
}

func main() {
	agent.Run(NewEC2Client(DefaultAwsRegion, 3))
}
