package main

import (
	"google.golang.org/grpc/grpclog"
	"io"
	"google.golang.org/grpc"
	pb "proto"
	"context"
)

func main() {
	conn, err := grpc.Dial("127.0.0.1:1234", grpc.WithInsecure())
	if err != nil {
		grpclog.Fatalf("fail to dial: %v", err)
	}
	defer conn.Close()
	client := pb.NewClusterClientServiceClient(conn)

	stream, err := client.ServerClusters(context.Background())
	if err != nil {
		grpclog.Fatalf("%v.ServerClusters(_) = _, %v", client, err)
	}
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
		}
	}()

	stream.Send(&pb.ClusterRequest{
		RequestId: "foo",
	})

	stream.CloseSend()
	<-waitc
}
