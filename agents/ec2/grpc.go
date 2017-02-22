package main

import (
	"context"
	"github.com/opennetworkinglab/onos-warden/warden"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/grpclog"
	"io"
)

type Handler interface {
	Handle(req *warden.ClusterRequest)
}

type WardenClient interface {
	PublishUpdate(ad *warden.ClusterAdvertisement)
	Teardown()
}

type wardenClient struct {
	conn   *grpc.ClientConn
	stream warden.ClusterAgentService_AgentClustersClient
	alive  bool
}

func NewWardenClient(target string, handler Handler, creds credentials.TransportCredentials) (*wardenClient, error) {
	var wc wardenClient
	var err error

	var opts grpc.DialOption
	if creds == nil {
		opts = grpc.WithInsecure()
	} else {
		opts = grpc.WithTransportCredentials(creds)
	}
	wc.conn, err = grpc.Dial(target, opts)
	if err != nil {
		grpclog.Fatalf("fail to dial: %v", err)
		return nil, err
	}
	client := warden.NewClusterAgentServiceClient(wc.conn)
	wc.stream, err = client.AgentClusters(context.Background())
	if err != nil {
		grpclog.Fatalf("%v.ServerClusters(_) = _, %v", client, err)
		wc.conn.Close()
		return nil, err
	}

	go func() {
		for {
			in, err := wc.stream.Recv()
			if err == io.EOF {
				// TODO server has closed their side of the connection, when do we stop sending updates?
				// let's stop now!
				wc.Teardown()
				return
			}
			if err != nil {
				grpclog.Fatalf("Failed to receive: %v", err)
			}
			grpclog.Printf("Got message: %v", in)
			if handler != nil {
				handler.Handle(in)
			}
		}
	}()
	wc.alive = true
	return &wc, nil
}

func (c *wardenClient) PublishUpdate(ad *warden.ClusterAdvertisement) {
	if c.alive == false {
		//TODO keep calm, don't panic
		panic("Trying to publish update to dead connection.")
	}
	err := c.stream.Send(ad)
	if err != nil {
		panic(err)
	}
}

func (c *wardenClient) Teardown() {
	c.alive = false
	if c.stream != nil {
		c.stream.CloseSend()
		c.stream = nil
	}
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}
