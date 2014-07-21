package main

import (
  "./ninja"
  "fmt"
  "github.com/bcurren/go-hue"
  "github.com/bitly/go-simplejson"
  "log"
  "math"
  "os"
  "os/signal"
  "strings"
  "time"
)

func getBridge() *hue.Bridge {
  nobridge := true
  var allbridges []*hue.Bridge
  for nobridge {
    allbridges, _ = hue.FindBridges()
    if len(allbridges) == 0 {
      log.Printf("Couldn't find bridge, retrying")
      time.Sleep(time.Second * 2) //this sucks
    } else {
      nobridge = false
      log.Printf("got %d bridges: %s", len(allbridges), allbridges)
    }
  }
  return allbridges[0]
}

func getUser(bridge *hue.Bridge) *hue.User {
  var user *hue.User
  var err error
  noUser := true
  retries := 0
  for noUser {
    user, err = bridge.CreateUser("ninjadevice", "ninjausername")
    if err != nil {
      if strings.Contains(err.Error(), "101") { // there's probably a nicer way to check this
        retries++
        log.Printf("Couldn't make user, push link button. Retry: %d", retries)
        time.Sleep(time.Second * 2) //this sucks
      } else {
        log.Fatalf("Error creating user: %s", err)
      }
    }

    if user != nil {
      noUser = false
    }
  }
  return user
}



func main() {



}
