package main

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/ninjasphere/go-ninja"
	"github.com/ninjasphere/go-ninja/logger"
)

const driverName = "driver-hue"

var log = logger.GetLogger(driverName)

func main() {

	log.Infof("Starting " + driverName)

	conn, err := ninja.Connect("com.ninjablocks.hue")
	if err != nil {
		log.HandleError(err, "Could not connect to MQTT")
	}

	pwd, _ := os.Getwd()

	bus, err := conn.AnnounceDriver("com.ninjablocks.hue", driverName, pwd)
	if err != nil {
		log.HandleError(err, "Could not get driver bus")
	}

	statusJob, err := ninja.CreateStatusJob(conn, driverName)

	if err != nil {
		log.HandleError(err, "Could not setup status job")
	}

	statusJob.Start()

	bridge := getBridge()
	user := getUser(bridge)

	allLights, err := user.GetLights()
	if err != nil {
		log.HandleError(err, "Couldn't get lights")
	}

	for _, l := range allLights {
		_, err := NewLight(&l, bus, bridge, user)
		if err != nil {
			log.HandleError(err, "Error creating light instance")
		}

	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill)

	// Block until a signal is received.
	s := <-c
	fmt.Println("Got signal:", s)

}
