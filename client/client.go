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

// Request the first available cell, return when cell reservation is completed
func main() {
	rand.Seed(time.Now().Unix())
	currUser, err := user.Current()
	if err != nil {
		panic(err)
	}
	username := flag.String("user", currUser.Username, "username for reservation; defaults to $USER")
	key := flag.String("key", "", "public key for SSH")
	duration := flag.Int64("duration", -1, "duration of reservation in minutes; -1 is unlimited")
	nodes := flag.Uint64("nodes", 3, "number of nodes in cell; defaults to 3")

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
		if cluster == nil {
			// pick the first available cluster
			if ad.State == warden.ClusterAdvertisement_AVAILABLE {
				cluster = ad
				reqId = fmt.Sprintf("res-%x", rand.Int())
				stream.Send(&warden.ClusterRequest{
					RequestId:   reqId,
					Type:        warden.ClusterRequest_RESERVE,
					ClusterId:   ad.ClusterId,
					ClusterType: ad.ClusterType,
					Duration:    int32(*duration),
					Spec: &warden.ClusterRequest_Spec{
						ControllerNodes: uint32(*nodes),
						UserName: *username,
						UserKey: *key,
					},
				})
			}

		} else if cluster.ClusterId == ad.ClusterId && cluster.ClusterType == cluster.ClusterType {
			if ad.RequestId != reqId {
				// cluster was assigned to someone else; try again...
				cluster = nil
				reqId = ""
			} else if ad.State == warden.ClusterAdvertisement_RESERVED {
				fmt.Println("Got cluster:", ad);
				break
			}
		}
	}
}
