package main

import (
	"context"
	"github.com/opennetworkinglab/onos-warden/warden"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"io"
	"fmt"
	"os"
	"os/user"
	"flag"
	"os/signal"
)

type client struct {
	stream warden.ClusterClientService_ServerClustersClient
	ads chan *warden.ClusterAdvertisement
}

func New(addr string) *client {
	c := client {
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
			//TODO remove this:
			grpclog.Println("Got message:", ad)
			c.ads <- ad
		}
	}()
	return &c
}

func (c *client) sendRequest(baseRequest warden.ClusterRequest, t warden.ClusterRequest_RequestType) {
	baseRequest.Type = t
	c.stream.Send(&baseRequest)
}

func (c *client) returnClusterAndExit(req warden.ClusterRequest, code int) {
	fmt.Println("returning...")
	c.sendRequest(req, warden.ClusterRequest_RETURN)
	c.stream.CloseSend()
	fmt.Println("return sent")
	os.Exit(code)
}

// Request the first available cell, return when cell reservation is completed or cell becomes unavailable
func main() {
	currUser, err := user.Current()
	if err != nil {
		panic(err)
	}
	username := flag.String("user", currUser.Username, "username for reservation; defaults to $USER")
	key := flag.String("key", "", "public key for SSH")
	duration := flag.Int64("duration", -1, "duration of reservation in minutes; -1 is unlimited")
	nodes := flag.Uint64("nodes", 3, "number of nodes in cell; defaults to 3")
	addr := flag.String("addr", "127.0.0.1:1234", "address of warden")

	//Note: Request ids must be unique
	// ClusterId and ClusterType are optional and we won't be filling those in
	reqId := *username //TODO just using the username for now
	baseRequest := warden.ClusterRequest{
		Duration:    int32(*duration),
		RequestId:   reqId,
		Spec: &warden.ClusterRequest_Spec{
			ControllerNodes: uint32(*nodes),
			UserName:        *username,
			UserKey:         *key,
		},
	}
	c := New(*addr)
	c.sendRequest(baseRequest, warden.ClusterRequest_RESERVE)

	intrChan := make(chan os.Signal)
	signal.Notify(intrChan, os.Interrupt, os.Kill)
	var cluster *warden.ClusterAdvertisement
	blockUntilInterrupt := true //TODO make this settable by flag; if false, require duration > 0

	for {
		select {
		case <-intrChan:
			c.returnClusterAndExit(baseRequest,0)
		case ad := <-c.ads:
			switch ad.State {
			case warden.ClusterAdvertisement_READY:
				//TODO ready logic
				fmt.Println("Ready cluster:", ad);
				if !blockUntilInterrupt {
					c.stream.CloseSend()
					return
				}
				fallthrough
			case warden.ClusterAdvertisement_RESERVED:
				if baseRequest.RequestId == ad.RequestId {
					// cluster is ready!
					if cluster == nil ||
						cluster.ClusterId != ad.ClusterId ||
						cluster.ClusterType != ad.ClusterType ||
						cluster.RequestId != ad.RequestId {
						fmt.Println("Got cluster:", ad);
						cluster = ad

					}
				}
			default: // warden.ClusterAdvertisement_{UNAVAILABLE, AVAILABLE}
				if cluster != nil &&
					cluster.ClusterId == ad.ClusterId &&
					cluster.ClusterType == ad.ClusterType {
					// our cluster is no longer available
					fmt.Println("Returning cluster, then exit error")
					c.returnClusterAndExit(baseRequest, 1)
				}
			}
		}
	}
}
