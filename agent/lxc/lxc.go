package main

import (
	"github.com/opennetworkinglab/onos-warden/agent"
	"github.com/opennetworkinglab/onos-warden/warden"
	"time"
	"os"
	"bufio"
	"fmt"
	"strings"
	"crypto/md5"
	"strconv"
	"encoding/hex"
)

const configurationFile = "/Users/tom/cells.cfg"
const maxCellSize = 20

type lxcCell struct {
	ad             warden.ClusterAdvertisement
	ipStart        string
	md5            string
	lastChanged    time.Time
	lastAdvertised time.Time
}

type lxcClient struct {
	client agent.WardenClient
	cells  map[string]lxcCell

	// TODO information about the local bare-metal, i.e. name, cell names, ip-ranges, etc.
}

// Creates a new LXC agent worker
func NewAgentWorker() (agent.Worker, error) {
	var c lxcClient
	c.cells = make(map[string]lxcCell)
	return &c, nil
}

func (c *lxcClient) Start() {
	go func() {
		for {
			c.readConfiguration()
			c.sendUpdates()
			time.Sleep(5 * time.Second)
		}
	}()
}

func (c *lxcClient) Bind(client agent.WardenClient) {
	c.client = client
}

func (c *lxcClient) Teardown() {
	//TODO
}

func (c *lxcClient) Handle(req *warden.ClusterRequest) {
	//TODO
}

// Reads the cell configuration file and populates this agent's internal structures.
func (c *lxcClient) readConfiguration() {
	f, err := os.Open(configurationFile)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		cell := c.cellConfig(scanner.Text())
		c.cells[cell.ad.ClusterId] = cell
	}
}

// Reads the current cell configuration from the environment.
func (c *lxcClient) cellConfig(cfg string) (lxcCell) {
	var cell lxcCell
	hash := md5.Sum([]byte(cfg))
	md5 := hex.EncodeToString(hash[:])

	// cellName, ipStart
	fields := strings.Split(cfg, ",")
	name, ipStart := fields[0], fields[1]

	cell.ad.ClusterId = name
	cell.ad.ClusterType = "onlab"
	cell.ad.HeadNodeIP = ipStart
	cell.ipStart = ipStart

	if cell.md5 != md5 {
		cell.lastChanged = time.Now()
	}

	cell.ad.ReservationInfo = c.readReservation(name)

	fmt.Println(cfg)
	return cell
}


// Reads the current reservation data from the current file; returns nil if there
// is no reservation.
func (c *lxcClient) readReservation(cell string) (*warden.ClusterAdvertisement_ReservationInfo) {
	f, err := os.Open("/Users/tom/" + cell + ".rez")
	if err != nil {
		return nil
	}
	defer f.Close()

	// read from file called '<cellName>.rez'
	// user, startTime, duration, cellSize
	scanner := bufio.NewScanner(f)
	scanner.Scan()
	fields := strings.Split(scanner.Text(), ",")

	startTime, err := strconv.ParseUint(strings.Trim(fields[1], " "), 10,32)
	if err != nil {
		panic("Invalid time:" + fields[1])
	}

	duration, err := strconv.ParseInt(strings.Trim(fields[2], " "), 10, 32)
	if err != nil {
		panic("Invalid duration")
	}

	return &warden.ClusterAdvertisement_ReservationInfo{
		UserName: fields[0],
		ReservationStartTime: uint32(startTime),
		Duration: int32(duration),
	}
}

// Sweeps through all the cells and sends an update if the cell status has
// changed since the last time we send an advertisement.
func (c *lxcClient) sendUpdates() {
	for _, cell := range c.cells {
		if cell.lastAdvertised.Before(cell.lastChanged) {
			c.client.PublishUpdate(&cell.ad)
			cell.lastAdvertised = time.Now()
		}
	}

}

// Runs Warden agent using LXC worker on internal bare-metal machines.
func main() {
	agent.Run(NewAgentWorker())
}
