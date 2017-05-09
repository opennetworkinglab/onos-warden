package agent

import (
	"bytes"
	"fmt"
	"golang.org/x/crypto/ssh"
	"io/ioutil"
)

// Returns an ssh.ClientConfig given a username and key filepath
func GetConfig(user, keyFile string) (*ssh.ClientConfig, error) {
	b, err := ioutil.ReadFile(keyFile)
	if err != nil {
		return nil, err
	}

	key, err := ssh.ParsePrivateKey(b)
	if err != nil {
		return nil, err
	}

	return &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(key)},
	}, nil
}

// Runs the given command on the ssh.Client and provides stdin to the processes on the remote
// Returns stdout and stderr; err is != nil if exit status is != 0
func RunCmd(c *ssh.Client, cmd, stdin string) (stdout, stderr string, err error) {
	session, err := c.NewSession()
	if err != nil {
		return
	}

	var outbuf, errbuf bytes.Buffer
	outwait, errwait := make(chan struct{}), make(chan struct{})
	outpipe, err := session.StdoutPipe()
	if err != nil {
		return
	}
	go func() {
		outbuf.ReadFrom(outpipe)
		close(outwait)
	}()

	errpipe, err := session.StderrPipe()
	if err != nil {
		return
	}
	go func() {
		errbuf.ReadFrom(errpipe)
		close(errwait)
	}()

	inpipe, err := session.StdinPipe()
	if err != nil {
		return
	}

	err = session.Start(cmd)
	if err != nil {
		fmt.Printf("Failed to run: %s", err)
		return
	}

	if stdin != "" {
		_, err = bytes.NewBufferString(stdin).WriteTo(inpipe)
		if err != nil {
			return
		}
	}
	inpipe.Close()

	// wait throws error if cmd return != 0
	cmdErr := session.Wait()
	<-outwait
	<-errwait
	session.Close()
	return outbuf.String(), errbuf.String(), cmdErr

}

func example() {
	addr := "54.153.91.176:22"

	user, key := "ubuntu", "/Users/bocon/.ssh/onos-warden.pem"

	fmt.Println("Dialing...")
	config, err := GetConfig(user, key)
	if err != nil {
		panic(err)
	}
	connection, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		fmt.Printf("Failed to dial: %s", err)
		return
	}

	var cmd, in, stdout, stderr string
	cmd, in = "date", ""
	stdout, stderr, err = RunCmd(connection, cmd, in)
	fmt.Printf("out: %s, err: %s, err: %v\n", stdout, stderr, err)
}
