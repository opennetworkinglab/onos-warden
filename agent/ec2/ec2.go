package main

import (
	"fmt"
	"github.com/opennetworkinglab/onos-warden/agent"
	"github.com/opennetworkinglab/onos-warden/warden"
	"time"
	"github.com/aws/aws-sdk-go/service/ec2"
	"sync"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/aws"
	"encoding/binary"
	"net"
	"reflect"
	"errors"
	"strconv"
	"golang.org/x/crypto/ssh"
	"io/ioutil"
)

type cluster struct{
	warden.ClusterAdvertisement
	Size uint32
	InstanceId string
	InstanceType string
	InstanceStarted bool
	LaunchTime time.Time
}

const (
	DefaultAwsRegion string = "us-west-1"
	ClusterType = "ec2"
	InstanceName = "warden-cell"
	InstanceImageId = "ami-a17128c1"
	InstanceType = "m3.medium"
	KeyName = "onos-warden"
	MaxPrice = "1" // $1/hr, TODO make this dynamic
	updatePollingInterval = 2 * time.Minute
	startupPollingInterval = 2 * time.Second
)

var IpBase = binary.BigEndian.Uint32(net.ParseIP("10.0.1.100")[12:16])


type ec2Client struct {
	svc      *ec2.EC2
	client   agent.WardenClient
	clusters map[string]cluster
	requests map[string]string
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
	c.requests = make(map[string]string)
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
			time.Sleep(updatePollingInterval)
			c.updateInstances()
		}
	}()
}

func (c *ec2Client) Teardown() {
	//TODO
	fmt.Println("teardown...")
}

func (c *ec2Client) Handle(req *warden.ClusterRequest) {
	if req.ClusterType != "" && req.ClusterType != ClusterType {
		fmt.Printf("Requested cluster type %s is not %s", req.ClusterType, ClusterType)
		return
	}

	switch req.Type {
	case warden.ClusterRequest_RESERVE:
		cl, err := c.reserveCluster(req)
		if err != nil {
			fmt.Println("Unable process reservation", req, err)
			return
		}
		c.provisionCluster(cl, req.Spec.UserKey)
	case warden.ClusterRequest_EXTEND:
		_, err := c.extendCluster(req)
		if err != nil {
			fmt.Println("Unable process extension", req, err)
			return
		}
	case warden.ClusterRequest_RETURN:
		fmt.Println("Got return", req)
		err := c.returnCluster(req)
		if err != nil {
			fmt.Println("Unable process return", req, err)
			return
		}
	default:
		fmt.Printf("Unsupported request: %+v\n", req)
	}
}

// You must hold c.mux before calling this method
func (c *ec2Client) addOrUpdate(cl cluster) {
	id := cl.ClusterId
	old, ok := c.clusters[id]
	c.clusters[id] = cl
	//TODO consider custom equal() instead of reflect
	if !ok || !reflect.DeepEqual(cl.ClusterAdvertisement, old.ClusterAdvertisement) {
		fmt.Printf("Updating: %+v\n", cl)
		c.client.PublishUpdate(&cl.ClusterAdvertisement)
	} else {
		fmt.Println("new or equal", id)
	}
	if old.RequestId == "" && cl.RequestId != "" {
		c.requests[cl.RequestId] = cl.ClusterId
	} else if old.RequestId != "" && cl.RequestId == "" {
		//TODO may want to publish unavailable
		delete(c.requests, old.RequestId)
	}

}

func (c *ec2Client) reserveCluster(req *warden.ClusterRequest) (*cluster, error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	cId := req.ClusterId
	if rId := req.RequestId; rId != "" {
		v, ok := c.requests[rId]
		if ok {
			if cId != "" && cId != v {
				// request is present; cId does not match request's cluster id
				return nil, errors.New("provided cluster id does not match reserved one")
			} else {
				// uses the request's existing cluster id
				cId = v
			}
		}
	}
	if cId != "" {
		v, ok := c.clusters[cId]
		if ok && (v.State == warden.ClusterAdvertisement_RESERVED || v.State == warden.ClusterAdvertisement_READY) {
			return &v, nil
		} else {
			fmt.Println("error... cluster is not reserved", v, req)
			return nil, errors.New("error")
		}
	}

	var cl, placeholder *cluster
	// reserve an available cluster
	for _, v := range c.clusters {
		if v.State == warden.ClusterAdvertisement_AVAILABLE {
			if v.InstanceId != "" {
				// return the first available, instantiated cell
				cl = &v
				break
			} else if placeholder == nil {
				placeholder = &v
			}
		}
	}

	if cl == nil && placeholder == nil {
		return nil, errors.New("no available clusters")
	} else if cl == nil {
		cl = placeholder
		err := c.makeSpotRequest(cl)
		if err != nil {
			return nil, err
		}
	}

	cl.State = warden.ClusterAdvertisement_RESERVED
	cl.Size = req.Spec.ControllerNodes
	cl.RequestId = req.RequestId
	cl.ReservationInfo = &warden.ClusterAdvertisement_ReservationInfo{
		UserName: req.Spec.UserName,
		Duration: req.Duration,
		ReservationStartTime: uint32(time.Now().Unix()),
	}

	c.tagInstance(cl.InstanceId, cl)
	c.addOrUpdate(*cl)
	return cl, nil
}

func (c *ec2Client) extendCluster(req *warden.ClusterRequest) (*cluster, error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	cId := req.ClusterId
	if rId := req.RequestId; rId != "" {
		v, ok := c.requests[rId]
		if ok {
			if cId != "" && cId != v {
				// request is present; cId does not match request's cluster id
				return nil, errors.New("provided cluster id does not match reserved one")
			} else {
				// uses the request's existing cluster id
				cId = v
			}
		}
	}
	cl, ok := c.clusters[cId]
	if !ok || cId == "" {
		return nil, errors.New("cluster not found")
	}

	if (cl.State == warden.ClusterAdvertisement_RESERVED ||
		cl.State == warden.ClusterAdvertisement_READY) &&
		cl.ReservationInfo != nil &&
		cl.RequestId == req.RequestId {
		// Update the duration field
		start := time.Unix(int64(cl.ReservationInfo.ReservationStartTime), int64(0))
		past := time.Since(start)
		newDuration := int32(float64(req.Duration) + past.Minutes())
		cl.ReservationInfo.Duration = newDuration

	} else {
		return nil, fmt.Errorf("Could not extend reservation %v", req)
	}

	c.tagInstance(cl.InstanceId, &cl)
	c.addOrUpdate(cl)
	return &cl, nil
}

func (c *ec2Client) returnCluster(req *warden.ClusterRequest) error {
	c.mux.Lock()
	defer c.mux.Unlock()

	cId := req.ClusterId
	if rId := req.RequestId; rId != "" {
		v, ok := c.requests[rId]
		if ok {
			if cId != "" && cId != v {
				// request is present; cId does not match request's cluster id
				return errors.New("provided cluster id does not match reserved one")
			} else {
				// uses the request's existing cluster id
				cId = v
			}
		}
	}
	cl, ok := c.clusters[cId]
	if !ok || cId == "" {
		return errors.New("cluster not found")
	}

	cl.RequestId = ""
	cl.State = warden.ClusterAdvertisement_AVAILABLE
	cl.ReservationInfo = nil
	c.tagInstance(cl.InstanceId, &cl)
	c.addOrUpdate(cl)
	return nil
}

//http://blog.ralch.com/tutorial/golang-ssh-connection/
func PublicKeyFile(file string) ssh.AuthMethod {
	buffer, err := ioutil.ReadFile(file)
	if err != nil {
		return nil
	}

	key, err := ssh.ParsePrivateKey(buffer)
	if err != nil {
		return nil
	}
	return ssh.PublicKeys(key)
}

func createContainer(c *ssh.Client, name, ip, baseImage string) {
	var stdout, stderr string
	var err error
	agent.RunCmd(c, fmt.Sprintf("sudo lxc-stop -n %s", name), "")
	agent.RunCmd(c, fmt.Sprintf("sudo lxc-destroy -n %s", name), "")
	_,_,err = agent.RunCmd(c, fmt.Sprintf("sudo lxc-copy -n %s -N %s", baseImage, name), "")
	fmt.Printf("out: %s, err: %s, err: %v\n", stdout, stderr, err)
	if err != nil {
		//FIXME error handling
		return
	}
	stdout, stderr, err = agent.RunCmd(c, fmt.Sprintf("sudo tee -a /var/lib/lxc/%s/config", name),
		fmt.Sprintf("lxc.network.ipv4 = %s/24\nlxc.network.ipv4.gateway = 10.0.1.1\n", ip))
	fmt.Printf("out: %s, err: %s, err: %v\n", stdout, stderr, err)
	stdout, stderr, err = agent.RunCmd(c, fmt.Sprintf("sudo lxc-start -d -n %s", name), "")
	fmt.Printf("out: %s, err: %s, err: %v\n", stdout, stderr, err)
	stdout, stderr, err = agent.RunCmd(c, fmt.Sprintf("sudo lxc-attach -n %s -- ping -c1 8.8.8.8", name), "")
	fmt.Printf("out: %s, err: %s, err: %v\n", stdout, stderr, err)

}

func (c *ec2Client) provisionCluster(cl *cluster, userKey string) {
	//FIXME do actual provisioning
	time.Sleep(5 * time.Second)
	fmt.Println("instance ready!!!", cl.InstanceId, cl.HeadNodeIP)

	// FIXME cl.IP is nil if cluster was just started
	addr := fmt.Sprintf("%s:%d", cl.HeadNodeIP, 22)
	user, key := "ubuntu", "/Users/bocon/.ssh/onos-warden.pem"

	fmt.Println("Dialing...")
	config, err := agent.GetConfig(user, key)
	if err != nil {
		panic(err)
	}
	var connection *ssh.Client
	for i := 0; i < 60; i++ {
		connection, err = ssh.Dial("tcp", addr, config)
		if err == nil {
			break
		} else {
			fmt.Printf("Failed to dial: %s\n", err)
			time.Sleep(startupPollingInterval)
		}
	}
	if connection == nil {
		fmt.Println("Can't provision!!!!")
		return
	}

	var wg sync.WaitGroup
	wg.Add(int(cl.Size) + 1)
	ip := IpBase
	go func(ipNum uint32) {
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, ipNum)
		createContainer(connection, "onos-n", ip.String(), "test-base")
		wg.Done()
	}(ip)
	for i := 1; i <= int(cl.Size); i++ {
		ip++
		go func(i int, ipNum uint32) {
			name := fmt.Sprintf("onos-%d", i)
			ip := make(net.IP, 4)
			binary.BigEndian.PutUint32(ip, ipNum)
			createContainer(connection, name, ip.String(), "ctrl-base")
			wg.Done()
		}(i, ip)
	}
	wg.Wait()

	cl.State = warden.ClusterAdvertisement_READY
	c.tagInstance(cl.InstanceId, cl)

	c.mux.Lock()
	defer c.mux.Unlock()
	c.addOrUpdate(*cl)
}

func main() {
	agent.Run(NewEC2Client(DefaultAwsRegion, 3))
}

// --- EC2 utility methods ---

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
	size := cl.Size
	tags := make([]*ec2.Tag, 0)
	tags = append(tags,
		tag("Cell-Id", id),
		tag("Name", InstanceName),
		tag("Cell-Size", strconv.FormatUint(uint64(size), 10)))

	fmt.Printf("%+v\n", *cl)
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

	fmt.Println(tags)
	_, err := c.svc.CreateTags(&ec2.CreateTagsInput{
		Resources: aws.StringSlice([]string{inst}),
		Tags: tags,
	})
	return err
}

func (c *ec2Client) makeSpotRequest(cl *cluster) error {
	if cl.InstanceId != "" {
		return errors.New("Instance already exists for this cluster")
	}

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
			ImageId: aws.String(InstanceImageId),
			InstanceType: aws.String(InstanceType),
			KeyName: aws.String(KeyName),
			SecurityGroupIds: aws.StringSlice([]string{"all open"}),
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
	for { // Wait for reservation to be fulfilled
		fmt.Println("Wait for reservation...")
		time.Sleep(startupPollingInterval)
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
				cl.InstanceId = *r.InstanceId
				break
			}
		}
		if cl.InstanceId != "" {
			break
		}
	}
	// OR consider...
	//c.svc.WaitUntilSpotInstanceRequestFulfilled(&ec2.DescribeSpotInstanceRequestsInput{
	//	SpotInstanceRequestIds: ids,
	//})

	for { // Wait for instance to start
		out, err := c.svc.DescribeInstances(&ec2.DescribeInstancesInput{
			InstanceIds: aws.StringSlice([]string{cl.InstanceId}),
		})
		if err != nil {
			fmt.Println(err)
			continue
		}
		fmt.Println(out)

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
			//c.addOrUpdate([]cluster{*targetCl}, false)
			break
		}
		fmt.Println("Wait for start...", targetCl)
		time.Sleep(startupPollingInterval)
	}
	// OR consider...
	//c.svc.WaitUntilInstanceRunning(&ec2.DescribeInstancesInput{
	//	InstanceIds: aws.StringSlice([]string{id}),
	//})
	fmt.Println("started", cl.InstanceId)
	return nil
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
					fmt.Println("polling", cl)
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
			provisioned = v == "true"
		}
	}
	if provisioned && c.State == warden.ClusterAdvertisement_RESERVED {
		c.State = warden.ClusterAdvertisement_READY
	}
	//FIXME index 0 is the network node
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

func shouldShutdown(cl *cluster) bool {
	if !cl.InstanceStarted || cl.State != warden.ClusterAdvertisement_AVAILABLE {
		return false
	}
	delta := time.Since(cl.LaunchTime)
	delta = delta - time.Duration(delta.Hours()) // remove the hours
	fmt.Println(time.Hour - delta)
	if time.Hour - delta <= updatePollingInterval {
		return true
	}
	return false
}

func (c *ec2Client) terminateInstance(cl cluster) {
	_, err := c.svc.TerminateInstances(&ec2.TerminateInstancesInput{
		InstanceIds: aws.StringSlice([]string{cl.InstanceId}),
	})
	if err != nil {
		fmt.Println(err)
	}

	c.mux.Lock()
	defer c.mux.Unlock()
	c.addOrUpdate(emptyCluster(cl.ClusterId))
}