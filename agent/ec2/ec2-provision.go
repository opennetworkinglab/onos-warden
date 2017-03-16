package main

import (
	"encoding/binary"
	"fmt"
	"github.com/opennetworkinglab/onos-warden/agent"
	"github.com/opennetworkinglab/onos-warden/warden"
	"golang.org/x/crypto/ssh"
	"io/ioutil"
	"net"
	"sync"
	"time"
)

func (c *ec2Client) provisionCluster(cl *cluster, userKey string) {
	//FIXME do actual provisioning
	time.Sleep(5 * time.Second)
	fmt.Println("instance ready!!!", cl.InstanceId, cl.HeadNodeIP)

	// FIXME cl.IP is nil if cluster was just started
	addr := fmt.Sprintf("%s:%d", cl.HeadNodeIP, 22)
	user, key := "ubuntu", "/Users/bocon/.ssh/onos-warden.pem"

	fmt.Println("Dialing...")
	config, err := agent.GetConfig(user, key)
	if err != nil {
		panic(err)
	}
	var connection *ssh.Client
	for i := 0; i < 60; i++ {
		connection, err = ssh.Dial("tcp", addr, config)
		if err == nil {
			break
		} else {
			fmt.Printf("Failed to dial: %s\n", err)
			time.Sleep(startupPollingInterval)
		}
	}
	if connection == nil {
		fmt.Println("Can't provision!!!!")
		return
	}

	var wg sync.WaitGroup
	wg.Add(int(cl.Size) + 1)
	ip := IpBase
	go func(ipNum uint32) {
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, ipNum)
		createContainer(connection, "onos-n", ip.String(), "test-base")
		wg.Done()
	}(ip)
	for i := 1; i <= int(cl.Size); i++ {
		ip++
		go func(i int, ipNum uint32) {
			name := fmt.Sprintf("onos-%d", i)
			ip := make(net.IP, 4)
			binary.BigEndian.PutUint32(ip, ipNum)
			createContainer(connection, name, ip.String(), "ctrl-base")
			wg.Done()
		}(i, ip)
	}
	wg.Wait()

	cl.State = warden.ClusterAdvertisement_READY
	c.tagInstance(cl.InstanceId, cl)

	c.mux.Lock()
	defer c.mux.Unlock()
	c.addOrUpdate(*cl)
}

func PublicKeyFile(file string) ssh.AuthMethod {
	buffer, err := ioutil.ReadFile(file)
	if err != nil {
		return nil
	}

	key, err := ssh.ParsePrivateKey(buffer)
	if err != nil {
		return nil
	}
	return ssh.PublicKeys(key)
}

func createContainer(c *ssh.Client, name, ip, baseImage string) {
	var stdout, stderr string
	var err error
	agent.RunCmd(c, fmt.Sprintf("sudo lxc-stop -n %s", name), "")
	agent.RunCmd(c, fmt.Sprintf("sudo lxc-destroy -n %s", name), "")
	_, _, err = agent.RunCmd(c, fmt.Sprintf("sudo lxc-copy -n %s -N %s", baseImage, name), "")
	fmt.Printf("out: %s, err: %s, err: %v\n", stdout, stderr, err)
	if err != nil {
		//FIXME error handling
		return
	}
	stdout, stderr, err = agent.RunCmd(c, fmt.Sprintf("sudo tee -a /var/lib/lxc/%s/config", name),
		fmt.Sprintf("lxc.network.ipv4 = %s/24\nlxc.network.ipv4.gateway = 10.0.1.1\n", ip))
	fmt.Printf("out: %s, err: %s, err: %v\n", stdout, stderr, err)
	stdout, stderr, err = agent.RunCmd(c, fmt.Sprintf("sudo lxc-start -d -n %s", name), "")
	fmt.Printf("out: %s, err: %s, err: %v\n", stdout, stderr, err)
	stdout, stderr, err = agent.RunCmd(c, fmt.Sprintf("sudo lxc-attach -n %s -- ping -c1 8.8.8.8", name), "")
	fmt.Printf("out: %s, err: %s, err: %v\n", stdout, stderr, err)

}
