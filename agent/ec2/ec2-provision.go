package main

import (
	"encoding/binary"
	"fmt"
	"github.com/opennetworkinglab/onos-warden/agent"
	"github.com/opennetworkinglab/onos-warden/warden"
	"golang.org/x/crypto/ssh"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

func writer(cl *cluster, name string) (io.Writer, error) {
	dirpath := fmt.Sprintf("/tmp/%s-%s/", cl.ClusterId, cl.ClusterType)
	err := os.MkdirAll(dirpath, 0755)
	if err != nil {
		return nil, err
	}
	filepath := dirpath + name + ".log"
	f, err := os.OpenFile(filepath, os.O_RDWR|os.O_CREATE, 0755)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (c *ec2Client) provisionCluster(cl *cluster, userPubKey string) error {
	fmt.Printf("Provisioning cluster %s (%s) at %s\n", cl.ClusterId, cl.InstanceId, cl.HeadNodeIP)
	var err error

	addr := fmt.Sprintf("%s:%d", cl.HeadNodeIP, 22)
	user, key := "ubuntu", "/Users/bocon/.ssh/onos-warden.pem"

	fmt.Print("Dialing...")
	config, err := agent.GetConfig(user, key)
	if err != nil {
		return err
	}
	var connection *ssh.Client
	for i := 0; i < 60; i++ {
		connection, err = ssh.Dial("tcp", addr, config)
		if err == nil {
			break
		} else {
			fmt.Print(".")
			time.Sleep(startupPollingInterval)
		}
	}
	if connection == nil {
		fmt.Println("Failed to dial:", err)
		return err
	} else {
		fmt.Println()
	}

	internalPrivKey, internalPubKey, err := agent.GenerateKeyPair()
	if err != nil {
		return err
	}

	//FIXME copy internal key into container
	//FIXME copy user key into container

	var wg sync.WaitGroup
	wg.Add(int(cl.Size) + 1)
	ip := IpBase
	go func(ipNum uint32) {
		name := "onos-n"
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, ipNum)
		log, err := writer(cl, name)
		if err {
			fmt.Println(err)
			return
		}
		createContainer(connection, log, name, ip.String(), "test-base")
		addKeyPair(connection, log, name, internalPrivKey, internalPubKey)
		addAuthorizedKey(connection, log, name, userPubKey)
		addAuthorizedKey(connection, log, name, internalPubKey)
		wg.Done()
	}(ip)
	for i := 1; i <= int(cl.Size); i++ {
		ip++
		go func(i int, ipNum uint32) {
			name := fmt.Sprintf("onos-%d", i)
			ip := make(net.IP, 4)
			binary.BigEndian.PutUint32(ip, ipNum)
			log, err := writer(cl, name)
			if err {
				fmt.Println(err)
				return
			}
			createContainer(connection, log, name, ip.String(), "ctrl-base")
			addAuthorizedKey(connection, log, name, internalPubKey)
			wg.Done()
		}(i, ip)
	}
	wg.Wait()

	cl.State = warden.ClusterAdvertisement_READY
	c.tagInstance(cl.InstanceId, cl)

	c.mux.Lock()
	defer c.mux.Unlock()
	c.addOrUpdate(*cl)
	return nil
}

func logAndRunCmd(c *ssh.Client, log io.Writer, cmd, stdin string) (err error) {
	var stdout, stderr string

	log.Write([]byte(cmd))
	if stdin != "" {
		log.Write([]byte(" < "))
		log.Write([]byte(stdin))
	}
	log.Write([]byte("\n"))
	stdout, stderr, err = agent.RunCmd(c, cmd, stdin)
	if stdout != "" {
		log.Write([]byte("STDOUT: "))
		log.Write([]byte(stdout))
		log.Write([]byte("\n"))
	}
	if stderr != "" {
		log.Write([]byte("STDERR: "))
		log.Write([]byte(stderr))
		log.Write([]byte("\n"))
	}
	return
}

func createContainer(c *ssh.Client, log io.Writer, name, ip, baseImage string) (err error) {
	logAndRunCmd(c, log, fmt.Sprintf("sudo lxc-stop -n %s", name), "")
	logAndRunCmd(c, log, fmt.Sprintf("sudo lxc-destroy -n %s", name), "")
	err = logAndRunCmd(c, log, fmt.Sprintf("sudo lxc-copy -n %s -N %s", baseImage, name), "")
	if err != nil {
		return
	}
	err = logAndRunCmd(c, log, fmt.Sprintf("sudo tee -a /var/lib/lxc/%s/config", name),
		fmt.Sprintf("lxc.network.ipv4 = %s/24\nlxc.network.ipv4.gateway = 10.0.1.1\n", ip))
	if err != nil {
		return
	}
	err = logAndRunCmd(c, log, fmt.Sprintf("sudo lxc-start -d -n %s", name), "")
	if err != nil {
		return
	}
	err = logAndRunCmd(c, log, fmt.Sprintf("sudo lxc-attach -n %s -- ping -c1 8.8.8.8", name), "")
	if err != nil {
		return
	}
	err = logAndRunCmd(c, log, fmt.Sprintf("sudo lxc-attach -n %s -- ping -c1 8.8.8.8", name), "")
	if err != nil {
		return
	}
	err = logAndRunCmd(c, log, fmt.Sprintf("sudo lxc-attach -n %s -- ping -c1 8.8.8.8", name), "")
	return
}

func addAuthorizedKey(c *ssh.Client, log io.Writer, name, pubKey string) (err error) {
	cmd := fmt.Sprintf("sudo lxc-attach -n %s -- tee -a ~/.ssh/authorized_keys", name)
	err = logAndRunCmd(c, log, cmd, pubKey)
	return
}

func addKeyPair(c *ssh.Client, log io.Writer, name, privKey, pubKey string) (err error) {
	var cmd string
	cmd = fmt.Sprintf("sudo lxc-attach -n %s -- tee ~/.ssh/id_rsa", name)
	err = logAndRunCmd(c, log, cmd, privKey)
	if err != nil {
		return
	}
	cmd = fmt.Sprintf("sudo lxc-attach -n %s -- chmod 400 ~/.ssh/id_rsa", name)
	err = logAndRunCmd(c, log, cmd, privKey)
	if err != nil {
		return
	}
	cmd = fmt.Sprintf("sudo lxc-attach -n %s -- tee ~/.ssh/id_rsa.pub", name)
	err = logAndRunCmd(c, log, cmd, pubKey)
	if err != nil {
		return
	}
	cmd = fmt.Sprintf("sudo lxc-attach -n %s -- chmod 400 ~/.ssh/id_rsa.pub", name)
	err = logAndRunCmd(c, log, cmd, privKey)
	return
}
