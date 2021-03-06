package main

import (
	"strings"
	"time"

	"github.com/ninjasphere/go-hue"
	"github.com/ninjasphere/go-ninja/channels"
	"github.com/ninjasphere/go-ninja/config"
	"github.com/ninjasphere/go-ninja/logger"
)

var log = logger.GetLogger("hue")

func getBridge() *hue.Bridge {
	nobridge := true
	var allbridges []*hue.Bridge
	var err error
	for nobridge {
		allbridges, err = hue.FindBridgesUsingCloud()
		if err != nil {
			//log.Infof("Warning: Failed finding bridges using cloud (%s). Falling back to ssdp.", err)
			allbridges, _ = hue.FindBridges()
		}
		if len(allbridges) == 0 {
			time.Sleep(time.Second * 5) //this sucks
		} else {
			nobridge = false
			log.Infof("Found %d bridges: %s", len(allbridges), allbridges)
		}
	}
	return allbridges[0]
}

var pushButtonNotification = channels.Notification{
	Title:    "Please press the pairing button on your Philips Hue base-station",
	Subtitle: "New Hue base-station found",
	Priority: channels.NotificationPriorityDefault,
	Category: channels.NotificationCategorySuggestion,
}

func getUser(driver *HueDriver, bridge *hue.Bridge) *hue.User {
	var user *hue.User
	var err error
	noUser := true
	retries := 0
	serial := config.Serial()
	username := serial + serial //username must be long 10-40 characters
	isvaliduser, err := bridge.IsValidUser(username)
	if err != nil {
		log.Warningf("Problem determining if hue user is valid")
	}

	var notificationTime time.Time

	if isvaliduser {
		user = hue.NewUserWithBridge(username, bridge)
	} else {
		for noUser {
			user, err = bridge.CreateUser("ninjadevice", username)
			if err != nil {
				if strings.Contains(err.Error(), "101") { // there's probably a nicer way to check this
					retries++
					log.Debugf("Couldn't make user, push link button. Retry: %d", retries)

					if time.Since(notificationTime) > time.Minute*5 {
						notificationTime = time.Now()
						driver.sendEvent("notification", pushButtonNotification)
					}

					time.Sleep(time.Second * 2) //this sucks

				} else {
					log.Debugf("Error creating user %s", err)
					time.Sleep(time.Second * 20)
				}
			}

			if user != nil {
				noUser = false
			}
		}
	}
	return user
}
