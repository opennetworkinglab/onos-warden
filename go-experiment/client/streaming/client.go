package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/opennetworkinglab/onos-warden/warden"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"io"
	"io/ioutil"
	"os"
	"os/signal"
	"os/user"
	"regexp"
)

type client struct {
	stream warden.ClusterClientService_ServerClustersClient
	ads    chan *warden.ClusterAdvertisement
}

func New(addr string) *client {
	c := client{
		ads: make(chan *warden.ClusterAdvertisement),
	}

	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		grpclog.Fatalf("fail to dial: %v", err)
	}
	client := warden.NewClusterClientServiceClient(conn)

	c.stream, err = client.ServerClusters(context.Background())
	if err != nil {
		grpclog.Fatalf("%v.ServerClusters(_) = _, %v", client, err)
	}

	go func() {
		for {
			ad, err := c.stream.Recv()
			if err == io.EOF {
				// stream closed
				grpclog.Fatalln("Stream closed unexpectedly")
			}
			if err != nil {
				grpclog.Fatalf("Failed to receive: %v", err)
			}
			c.ads <- ad
		}
	}()
	return &c
}

func (c *client) sendRequest(baseRequest warden.ClusterRequest, t warden.ClusterRequest_RequestType) {
	baseRequest.Type = t
	c.stream.Send(&baseRequest)
}

func match(a, b *warden.ClusterAdvertisement) bool {
	if a == nil || b == nil {
		return false
	}
	return a.ClusterId == b.ClusterId && a.ClusterType == b.ClusterType
}

func (c *client) waitCluster(baseRequest warden.ClusterRequest) *warden.ClusterAdvertisement {
	intrChan := make(chan os.Signal)
	signal.Notify(intrChan, os.Interrupt, os.Kill)
	var cluster *warden.ClusterAdvertisement
	for {
		select {
		case <-intrChan:
			os.Exit(1)
		case ad := <-c.ads:
			if match(ad, cluster) {
				switch ad.State {
				case warden.ClusterAdvertisement_AVAILABLE:
					fallthrough
				case warden.ClusterAdvertisement_UNAVAILABLE:
					fmt.Println("Cluster no longer available")
					c.sendRequest(baseRequest, warden.ClusterRequest_RETURN)
					os.Exit(1)
				}
			}

			if ad.RequestId == baseRequest.RequestId {
				cluster = ad
				if ad.State == warden.ClusterAdvertisement_READY {
					return cluster
				} else if ad.State == warden.ClusterAdvertisement_RESERVED {
					//fmt.Println("Waiting for", cluster)
				}
			}
		}
	}
}

func printCell(cl *warden.ClusterAdvertisement) {
	fmt.Println("export ONOS_CELL=borrow")

	fmt.Printf("export OCT=%s\n", cl.HeadNodeIP)
	for _, n := range cl.Nodes {
		if n.Id == 0 {
			fmt.Printf("export OCN=%s\n", n.Ip)
			continue
		}
		if n.Id == 1 {
			r, err := regexp.Compile(".[0-9]+$")
			if err != nil {
				panic(err)
			}
			nic := r.ReplaceAllString(n.Ip, ".*")
			fmt.Printf("export ONOS_NIC=\"%s\"\n", nic)
		}
		fmt.Printf("export OC%d=%s\n", n.Id, n.Ip)
	}

	fmt.Println("export ONOS_USER=sdn")
	fmt.Println("export ONOS_USE_SSH=true")
	fmt.Println("export ONOS_APPS=drivers,openflow,proxyarp,mobility,pathpainter")
	fmt.Println("export ONOS_WEB_USER=onos")
	fmt.Println("export ONOS_WEB_PASS=rocks")
}

func main() {
	currUser, err := user.Current()
	if err != nil {
		panic(err)
	}
	defaultKey := fmt.Sprintf("%s/.ssh/id_rsa.pub", currUser.HomeDir)

	username := flag.String("user", currUser.Username, "username for reservation")
	key := flag.String("key", defaultKey, "public key for SSH")
	duration := flag.Int64("duration", 60, "duration of reservation in minutes")
	nodes := flag.Uint64("nodes", 3, "number of nodes in cell")
	addr := flag.String("addr", "127.0.0.1:1234", "address of warden")
	reqId := flag.String("reqId", currUser.Username, "request id for reservation")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] {reserve,status,return}\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}
	op := flag.Args()[0]

	keystr, err := ioutil.ReadFile(*key)
	if err != nil {
		fmt.Println("Error reading key:", *key)
		os.Exit(1)
	}

	// ClusterId and ClusterType are optional and we won't be filling those in
	req := warden.ClusterRequest{
		Duration:  int32(*duration),
		RequestId: *reqId,
		Spec: &warden.ClusterRequest_Spec{
			ControllerNodes: uint32(*nodes),
			UserName:        *username,
			UserKey:         string(keystr),
		},
	}

	c := New(*addr)
	defer c.stream.CloseSend()
	switch op {
	case "reserve":
		c.sendRequest(req, warden.ClusterRequest_RESERVE)
	case "return":
		c.sendRequest(req, warden.ClusterRequest_RETURN)
	case "status":
		cl := c.waitCluster(req)
		printCell(cl)
	}
}
