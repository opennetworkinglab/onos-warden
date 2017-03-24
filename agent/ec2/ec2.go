package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/opennetworkinglab/onos-warden/agent"
	"github.com/opennetworkinglab/onos-warden/warden"
	"net"
	"os"
	"reflect"
	"sync"
	"time"
)

type cluster struct {
	warden.ClusterAdvertisement
	Size            uint32
	InstanceId      string
	InstanceType    string
	InstanceStarted bool
	LaunchTime      time.Time
}

const (
	DefaultAwsRegion       string = "us-west-1"
	ClusterType                   = "ec2"
	InstanceName                  = "warden-cell"
	InstanceImageId               = "ami-3fcb935f"
	InstanceType                  = "m3.xlarge"
	KeyName                       = "onos-warden"
	MaxPrice                      = "1" // $1/hr, TODO make this dynamic
	updatePollingInterval         = 2 * time.Minute
	startupPollingInterval        = 2 * time.Second
)

var IpBase = binary.BigEndian.Uint32(net.ParseIP("10.0.1.100")[12:16])

type ec2Client struct {
	svc        *ec2.EC2
	client     agent.WardenClient
	clusters   map[string]cluster
	requests   map[string]string
	limit      int
	mux        sync.Mutex
	ec2User    string
	ec2KeyFile string
}

func NewEC2Client(region string, limit int) (*ec2Client, error) {
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
			fmt.Println("Unable reserve cluster for request", req, err)
			return
		}
		err = c.provisionCluster(cl, req.Spec.UserKey)
		if err != nil {
			fmt.Println("Unable to provision cluster for request", req, err)
			return
		}

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
		UserName:             req.Spec.UserName,
		Duration:             req.Duration,
		ReservationStartTime: time.Now().Unix(),
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

func main() {
	c, cErr := NewEC2Client(DefaultAwsRegion, 3)
	flag.StringVar(&c.ec2KeyFile, "keyFile", "", "Private key file used to ssh into newly created EC2 instances")
	flag.StringVar(&c.ec2User, "user", "ubuntu", "Username used to ssh into newly created EC2 instances")
	flag.Parse()
	if c.ec2KeyFile == "" {
		flag.Usage()
		os.Exit(1)
	} else if _, err := os.Stat(c.ec2KeyFile); err != nil {
		fmt.Fprintln(os.Stderr, "Key file not found:", c.ec2KeyFile)
		os.Exit(1)
	}
	agent.Run(c, cErr)
}

func getPlaceholderCluster(i int) cluster {
	i = i % 26
	name := agent.GetWord(string(rune('a' + i)))
	return emptyCluster(name)
}

func emptyCluster(id string) cluster {
	return cluster{
		ClusterAdvertisement: warden.ClusterAdvertisement{
			ClusterType: ClusterType,
			State:       warden.ClusterAdvertisement_AVAILABLE,
			ClusterId:   id,
		},
	}
}

func shouldShutdown(cl *cluster) bool {
	if !cl.InstanceStarted || cl.State != warden.ClusterAdvertisement_AVAILABLE {
		return false
	}
	delta := time.Since(cl.LaunchTime) % time.Hour // remove the hours
	if time.Hour-delta <= updatePollingInterval {
		return true
	}
	return false
}
