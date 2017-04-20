package agent

import (
	"context"
	"github.com/opennetworkinglab/onos-warden/warden"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/grpclog"
	"io"
	"time"
	"fmt"
)

type Handler interface {
	Handle(req *warden.ClusterRequest)
}

type WardenClient interface {
	PublishUpdate(ad *warden.ClusterAdvertisement) error
	Teardown()
}

type wardenClient struct {
	conn   *grpc.ClientConn
	stream warden.ClusterAgentService_AgentClustersClient
	alive  bool
	pub    chan *warden.ClusterAdvertisement
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

	err = wc.connect(target, opts)
	if err != nil {
		return nil, err
	}

	go func() {
		for {
			err := wc.receive(handler)
			fmt.Println("Receive error", err)
			// try to reconnect on receive error
			for i := 0; ; i++ {
				err = wc.connect(target, opts)
				if err == nil {
					break
				}
				fmt.Println("Connect error error; retrying...", err)
				time.Sleep(time.Duration(i) * 100 * time.Millisecond)
			}
		}
	}()

	return &wc, nil
}

func (c *wardenClient) connect(target string, opts grpc.DialOption) (err error) {
	c.conn, err = grpc.Dial(target, opts)
	if err != nil {
		grpclog.Printf("fail to dial: %v", err)
		return
	}
	client := warden.NewClusterAgentServiceClient(c.conn)
	c.stream, err = client.AgentClusters(context.Background())
	if err != nil {
		grpclog.Printf("failed to start stream: %v", err)
		c.conn.Close()
		return
	}
	return nil
}

func (c *wardenClient) receive(handler Handler) error {
	for {
		in, err := c.stream.Recv()
		if err == io.EOF {
			// TODO server has closed their side of the connection, when do we stop sending updates?
			// let's stop now!
			grpclog.Printf("Received EOF from server; tearing down...")
			c.Teardown()
			return err
		}
		if err != nil {
			grpclog.Printf("Failed to receive: %v", err)
			return err
		}
		grpclog.Printf("Got message: %v", in)
		if handler != nil {
			handler.Handle(in)
		}
	}
}

func (c *wardenClient) PublishUpdate(ad *warden.ClusterAdvertisement) (err error) {
	// TODO this is pretty rudimentary
	// retry 3 times with back-off
	for i := 1; i <= 3; i++ {
		err = c.stream.Send(ad)
		if err == nil {
			return
		}
		time.Sleep(time.Duration(i) * 100 * time.Millisecond)
	}
	return
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
