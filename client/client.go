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
	"math/rand"
	"time"
)


func init() {
	rand.Seed(time.Now().Unix())
}

func sendRequest(stream warden.ClusterClientService_ServerClustersClient, request warden.ClusterRequest, cId, cType string) (reqId string) {
	reqId = fmt.Sprintf("res-%4x", uint16(rand.Int()))
	request.ClusterId = cId
	request.ClusterType = cType
	request.RequestId = reqId
	stream.Send(&request)
	return
}

// Request the first available cell, return when cell reservation is completed
func main() {
	currUser, err := user.Current()
	if err != nil {
		panic(err)
	}
	username := flag.String("user", currUser.Username, "username for reservation; defaults to $USER")
	key := flag.String("key", "", "public key for SSH")
	duration := flag.Int64("duration", -1, "duration of reservation in minutes; -1 is unlimited")
	nodes := flag.Uint64("nodes", 3, "number of nodes in cell; defaults to 3")

	request := warden.ClusterRequest{
		Type:        warden.ClusterRequest_RESERVE,
		Duration:    int32(*duration),
		Spec: &warden.ClusterRequest_Spec{
			ControllerNodes: uint32(*nodes),
			UserName:        *username,
			UserKey:         *key,
		},
	}

	//TODO change this to a flag
	conn, err := grpc.Dial("127.0.0.1:1234", grpc.WithInsecure())
	if err != nil {
		grpclog.Fatalf("fail to dial: %v", err)
	}
	defer conn.Close()
	client := warden.NewClusterClientServiceClient(conn)

	stream, err := client.ServerClusters(context.Background())
	if err != nil {
		grpclog.Fatalf("%v.ServerClusters(_) = _, %v", client, err)
	}
	defer stream.CloseSend()

	var cluster *warden.ClusterAdvertisement
	var reqId string
	availableClusters := make([]*warden.ClusterAdvertisement, 0)
	for {
		ad, err := stream.Recv()
		if err == io.EOF {
			// stream closed
			grpclog.Fatalln("Stream closed unexpectedly")
			os.Exit(1)
		}
		if err != nil {
			grpclog.Fatalf("Failed to receive: %v", err)
		}
		grpclog.Println("Got message:", ad)
		switch ad.State {
		case warden.ClusterAdvertisement_AVAILABLE:
			if cluster == nil {
				// pick the first available cluster
				cluster = ad
				reqId = sendRequest(stream, request, ad.ClusterId, ad.ClusterType)
			} else {
				// store the cluster away for later, just in case the current one doesn't work out
				availableClusters = append(availableClusters, ad)
			}
		case warden.ClusterAdvertisement_RESERVED:
			if cluster != nil {
				if ad.RequestId != reqId {
					// cluster was assigned to someone else; try again...
					avail := availableClusters[0]
					availableClusters = availableClusters[1:]
					cluster = avail
					reqId = sendRequest(stream, request, avail.ClusterId, avail.ClusterType)
				} else if ad.State == warden.ClusterAdvertisement_RESERVED {
					// cluster is ready!
					fmt.Println("Got cluster:", ad);
					return
				}
			}
		}
	}
}
