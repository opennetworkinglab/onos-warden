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

const SshPort = 822

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

func (c *ec2Client) dialCluster(cl *cluster) (connection *ssh.Client, err error) {
	addr := fmt.Sprintf("%s:%d", cl.HeadNodeIP, SshPort)
	fmt.Print("Dialing...")
	config, err := agent.GetConfig(c.ec2User, c.ec2KeyFile)
	if err != nil {
		return
	}
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
		return
	} else {
		fmt.Println()
	}
	return
}


func (c *ec2Client) provisionCluster(cl *cluster, userPubKey string) (err error) {
	fmt.Printf("Provisioning cluster %s (%s) at %s\n", cl.ClusterId, cl.InstanceId, cl.HeadNodeIP)
	connection, err := c.dialCluster(cl)
	if err != nil {
		return err
	}

	internalPrivKey, internalPubKey, err := agent.GenerateKeyPair()
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	ip := IpBase
	//TODO this can be async if acceptHostKey is done after wait group
	func(ipNum uint32) {
		name := "onos-n"
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, ipNum)
		log, err := writer(cl, name)
		if err != nil {
			fmt.Println(err)
			return
		}
		createContainer(connection, log, name, ip.String(), "test-base")
		addKeyPair(connection, log, name, internalPrivKey, internalPubKey)
		addAuthorizedKey(connection, log, name, userPubKey)
		addAuthorizedKey(connection, log, name, internalPubKey)
		acceptHostKey(connection, log, name, ip.String())
		//wg.Done() TODO add this back if we make this async
	}(ip)
	wg.Add(int(cl.Size)) // wait for onos instance containers
	for i := 1; i <= int(cl.Size); i++ {
		ip++
		go func(i int, ipNum uint32) {
			name := fmt.Sprintf("onos-%d", i)
			ip := make(net.IP, 4)
			binary.BigEndian.PutUint32(ip, ipNum)
			log, err := writer(cl, name)
			if err != nil {
				fmt.Println(err)
				return
			}
			createContainer(connection, log, name, ip.String(), "ctrl-base")
			addKeyPair(connection, log, name, internalPrivKey, internalPubKey)
			addAuthorizedKey(connection, log, name, userPubKey)
			addAuthorizedKey(connection, log, name, internalPubKey)
			acceptHostKey(connection, log, "onos-n", ip.String())
			wg.Done()
		}(i, ip)
	}
	wg.Wait()

	cl.State = warden.ClusterAdvertisement_READY
	c.tagInstance(cl.InstanceId, cl)

	//FIXME there is something going on here where state != READY
	updatedCl, err := c.getInstance(cl.InstanceId)
	if err != nil {
		return err
	}
	c.mux.Lock()
	defer c.mux.Unlock()
	c.addOrUpdate(*updatedCl)
	return nil
}

func (c *ec2Client) destroyCluster(cl *cluster) error {
	fmt.Printf("Returning cluster %s (%s) at %s\n", cl.ClusterId, cl.InstanceId, cl.HeadNodeIP)
	connection, err := c.dialCluster(cl)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(int(cl.Size + 1))
	ip := IpBase
	go func(ipNum uint32) {
		name := "onos-n"
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, ipNum)
		log, err := writer(cl, name)
		if err != nil {
			fmt.Println(err)
			return
		}
		destroyContainer(connection, log, name, false)
		wg.Done()
	}(ip)
	// wait for onos instance containers
	for i := 1; i <= int(cl.Size); i++ {
		ip++
		go func(i int, ipNum uint32) {
			name := fmt.Sprintf("onos-%d", i)
			ip := make(net.IP, 4)
			binary.BigEndian.PutUint32(ip, ipNum)
			log, err := writer(cl, name)
			if err != nil {
				fmt.Println(err)
				return
			}
			destroyContainer(connection, log, name, false)
			wg.Done()
		}(i, ip)
	}
	wg.Wait()
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
	// destroy the container if it already exists
	destroyContainer(c, log, name, false)

	//TODO make snap0 configurable
	err = logAndRunCmd(c, log, fmt.Sprintf("sudo lxc-copy -n %s -s snap0 -B overlay -N %s", baseImage, name), "")
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
	err = logAndRunCmd(c, log,
		fmt.Sprintf("sudo lxc-attach -n %s -- sed -i \"s/127.0.1.1.*/127.0.1.1   %s/\" /etc/hosts", name, name),
		"")
	if err != nil {
		return
	}
	err = logAndRunCmd(c, log, fmt.Sprintf("sudo lxc-attach -n %s -- ping -c1 8.8.8.8", name), "")
	return
}

func destroyContainer(c *ssh.Client, log io.Writer, name string, failOnError bool) error {
	var err error
	err = logAndRunCmd(c, log, fmt.Sprintf("sudo lxc-stop -n %s", name), "")
	if err != nil && failOnError {
		return err
	}
	err = logAndRunCmd(c, log, fmt.Sprintf("sudo lxc-destroy -n %s", name), "")
	if err != nil && failOnError {
		return err
	}
	return nil
}

func addAuthorizedKey(c *ssh.Client, log io.Writer, name, pubKey string) (err error) {
	//TODO make the user configurable
	cmd := fmt.Sprintf("sudo lxc-attach -n %s -- tee -a /home/sdn/.ssh/authorized_keys", name)
	err = logAndRunCmd(c, log, cmd, pubKey)
	return
}

func addKeyPair(c *ssh.Client, log io.Writer, name, privKey, pubKey string) (err error) {
	var cmd string
	//TODO make the user configurable
	cmd = fmt.Sprintf("sudo lxc-attach -n %s -- tee /home/sdn/.ssh/id_rsa", name)
	err = logAndRunCmd(c, log, cmd, privKey)
	if err != nil {
		return
	}
	cmd = fmt.Sprintf("sudo lxc-attach -n %s -- chmod 400 /home/sdn/.ssh/id_rsa", name)
	err = logAndRunCmd(c, log, cmd, "")
	if err != nil {
		return
	}
	cmd = fmt.Sprintf("sudo lxc-attach -n %s -- chown sdn:sdn /home/sdn/.ssh/id_rsa", name)
	err = logAndRunCmd(c, log, cmd, "")
	if err != nil {
		return
	}
	cmd = fmt.Sprintf("sudo lxc-attach -n %s -- tee /home/sdn/.ssh/id_rsa.pub", name)
	err = logAndRunCmd(c, log, cmd, pubKey)
	if err != nil {
		return
	}
	cmd = fmt.Sprintf("sudo lxc-attach -n %s -- chmod 400 /home/sdn/.ssh/id_rsa.pub", name)
	err = logAndRunCmd(c, log, cmd, "")
	if err != nil {
		return
	}
	cmd = fmt.Sprintf("sudo lxc-attach -n %s -- chown sdn:sdn /home/sdn/.ssh/id_rsa.pub", name)
	err = logAndRunCmd(c, log, cmd, "")
	return
}

func acceptHostKey(c *ssh.Client, log io.Writer, name, remoteIp string) error {
	//TODO make user dynamic
	cmd := fmt.Sprintf("sudo lxc-attach -n %s -- sudo -u sdn ssh -n -o StrictHostKeyChecking=no -o PasswordAuthentication=no sdn@%s hostname", name, remoteIp)
	return logAndRunCmd(c, log, cmd, "")
}
