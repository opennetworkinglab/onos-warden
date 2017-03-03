package main

import (
	"context"
	"github.com/opennetworkinglab/onos-warden/warden"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"io"
	"fmt"
)

func main() {
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

	count := 0
	waitc := make(chan struct{})
	go func() {
		for {
			in, err := stream.Recv()
			if err == io.EOF {
				// read done.
				close(waitc)
				return
			}
			if err != nil {
				grpclog.Fatalf("Failed to receive: %v", err)
			}
			grpclog.Println("Got message:", in)
			if in.State == warden.ClusterAdvertisement_AVAILABLE {
				count++
				stream.Send(&warden.ClusterRequest{
					RequestId: fmt.Sprintf("res-%d", count),
					Type: warden.ClusterRequest_RESERVE,
					ClusterId: in.ClusterId,
					ClusterType: in.ClusterType,
					Duration: 0,
					Spec: &warden.ClusterRequest_Spec{
						ControllerNodes: uint32(3),
						UserName: "client",
						UserKey: "myawesomepubkey-rsa",
					},
				})
				stream.CloseSend()
				close(waitc)
				break
			}
		}
	}()

	//stream.CloseSend()
	<-waitc
}
