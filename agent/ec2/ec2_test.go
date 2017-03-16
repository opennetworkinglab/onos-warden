package main

import (
	"testing"
	"time"
	"github.com/opennetworkinglab/onos-warden/warden"
	"fmt"
)

func TestShouldShutdown(t *testing.T) {
		//t.Error("Expected 1.5, got ", v)
	testStart := time.Now()
	baseCluster := cluster{
		LaunchTime: testStart,
		InstanceStarted: true,
	}
	baseCluster.State = warden.ClusterAdvertisement_AVAILABLE

	t.Run("not started, not available", func (t *testing.T) {
		c := baseCluster
		c.InstanceStarted = false
		c.State = warden.ClusterAdvertisement_UNAVAILABLE
		if shouldShutdown(&c) {
			t.Error("Should shutdown returned true, expected false")
		}
	})
	t.Run("started, not available", func (t *testing.T) {
		c := baseCluster
		c.State = warden.ClusterAdvertisement_UNAVAILABLE
		if shouldShutdown(&c) {
			t.Error("Should shutdown returned true, expected false")
		}
	})
	t.Run("not started, available", func (t *testing.T) {
		c := baseCluster
		c.InstanceStarted = false
		if shouldShutdown(&c) {
			t.Error("Should shutdown returned true, expected false")
		}
	})

	// the remaining cases are: started = true && AVAILABLE
	for i := 0; i <= 180; i++ {
		n := fmt.Sprintf("Now - %d min, available", i)
		t.Run(n, func(m int) func(t *testing.T) {
			return func(t *testing.T) {
				expected := 60 - (i % 60) <= int(updatePollingInterval / time.Minute)
				delta := time.Duration(-i * int(time.Minute))
				c := baseCluster
				c.LaunchTime = time.Now().Add(delta)
				actual := shouldShutdown(&c)
				if actual != expected {
					t.Errorf("Now - %d min: Expected %v, Actual %v", i, expected, actual)
				}

			}
		}(i))
	}
}
