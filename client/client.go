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
	"os/user"
	"regexp"
	"time"
)

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

func printCluster(cl *warden.ClusterAdvertisement) {
	fmt.Printf("%+v\n", cl)
}

func sendRequest(req *warden.ClusterRequest,
	client warden.ClusterClientServiceClient,
	ctx context.Context) (reply chan struct{}) {
	reply = make(chan struct{})
	go func() {
		ad, err := client.Request(ctx, req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Requst failed: %v\n", err)
		} else {
			printCell(ad)
		}
		close(reply)
	}()
	return
}

func listClusters(client warden.ClusterClientServiceClient, ctx context.Context) (wait chan struct{}) {
	wait = make(chan struct{})
	go func() {
		stream, err := client.List(ctx, &warden.Empty{})
		if err != nil {
			fmt.Fprint(os.Stderr, "Requst failed: %v\n", err)
			return
		}

		for {
			ad, err := stream.Recv()
			if err == io.EOF {
				// stream closed
				break
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to receive: %v\n", err)
				break
			}
			printCluster(ad)
		}
		close(wait)
	}()
	return
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
	timeout := flag.Int64("timeout", -1, "duration in seconds to wait for reply; -1 for indefinitely")
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

	conn, err := grpc.Dial(*addr, grpc.WithInsecure())
	if err != nil {
		grpclog.Fatalf("fail to dial: %v", err)
	}
	defer conn.Close()

	client := warden.NewClusterClientServiceClient(conn)

	var waitReq chan struct{}
	ctx, cancel := context.WithCancel(context.Background())
	switch op {
	case "reserve":
		req.Type = warden.ClusterRequest_RESERVE
		waitReq = sendRequest(&req, client, ctx)
	case "return":
		req.Type = warden.ClusterRequest_RETURN
		waitReq = sendRequest(&req, client, ctx)
	case "extend":
		req.Type = warden.ClusterRequest_EXTEND
		waitReq = sendRequest(&req, client, ctx)
	case "status":
		fallthrough
	case "cell":
		req.Type = warden.ClusterRequest_STATUS
		waitReq = sendRequest(&req, client, ctx)
	case "list":
		waitReq = listClusters(client, ctx)
	}

	if waitReq != nil {
		if *timeout < 0 {
			<-waitReq
		} else if *timeout == 0 {
			return
		} else {
			select {
			case <-time.After(time.Duration(*timeout) * time.Second):
				// Timed out before request completed; Cancel request and wait for error
				cancel()
				<-waitReq
			case <-waitReq:
				// Request completed before timeout
				break
			}
		}
	}
}
