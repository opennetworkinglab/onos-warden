package util

import (
	"os"
	"os/signal"
)

// Block and wait for SIGINT or SIGKILL
func WaitForInterrupt() os.Signal {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, os.Kill)
	return <-c
}
