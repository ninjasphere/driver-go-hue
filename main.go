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

var _ = fmt.Printf

type Light struct {
	Id            string
	Name          string
	Bus           *ninja.DeviceBus
	OnOffBus      *ninja.ChannelBus
	colorBus      *ninja.ChannelBus
	brightnessBus *ninja.ChannelBus
	Bridge        *hue.Bridge
	User          *hue.User
	LightState    *hue.LightState
}

func (fl Light) SendOnOffState(state bool) error {

	js, _ := simplejson.NewJson([]byte(`{
    "state": ""
  }`))

	js.Set("state", state)

	return fl.OnOffBus.SendEvent("state", js)

}

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
	username := ninja.GetSerial() + ninja.GetSerial() //username must be long 10-40 characters

	isvaliduser, err := bridge.IsValidUser(username)
	if err != nil {
		log.Printf("Problem determining if hue user is valid")
	}

	if isvaliduser {
		user = hue.NewUserWithBridge(username, bridge)
	} else {
		for noUser {
			user, err = bridge.CreateUser("ninjadevice", username)
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
	}
	return user
}

func NewLight(l *hue.Light, bus *ninja.DriverBus, bridge *hue.Bridge, user *hue.User) (*Light, error) { //TODO cut this down!

	lightState := createLightState()

	light := &Light{
		Id:         l.Id,
		Name:       l.Name,
		Bridge:     bridge,
		User:       user,
		LightState: lightState,
	}

	sigs, err := simplejson.NewJson([]byte(`{
			"ninja:manufacturer": "Phillips",
			"ninja:productName": "Hue",
			"manufacturer:productModelId": "",
			"ninja:productType": "Light",
			"ninja:thingType": "light"
	}`))

	if err != nil {
		log.Fatalf("Bad sig json: %s", err)
	}

	la, err := user.GetLightAttributes(l.Id)
	if err != nil {
		log.Fatalf("Bad sig json: %s", err)
	}
	sigs.Set("manufacturer:productModelId", la.ModelId)

	log.Printf("Made light, id: %s, name: %s, signature: %s", l.Id, l.Name, sigs)

	deviceBus, _ := bus.AnnounceDevice(l.Id, "hue", l.Name, sigs)
	light.Bus = deviceBus

	methods := []string{"turnOn", "turnOff", "set"}
	events := []string{"state"}
	onOffBus, _ := light.Bus.AnnounceChannel("on-off", "on-off", methods, events, func(method string, payload *simplejson.Json) {

		switch method {
		case "turnOn":
			log.Printf("Turning on!")
			light.turnOnOff(true)
		case "turnOff":
			log.Printf("Turning off!")
			light.turnOnOff(false)
		case "set":
			state, _ := payload.GetIndex(0).Bool()
			log.Printf("Setting to %t!", state)
			light.turnOnOff(state)
		default:
			log.Printf("On-off got an unknown method %s", method)
		}

	})
	light.OnOffBus = onOffBus

	methods = []string{"set"}
	brightnessBus, _ := light.Bus.AnnounceChannel("brightness", "brightness", methods, events, func(method string, payload *simplejson.Json) {

		switch method {
		case "set":
			brightness, _ := payload.GetIndex(0).Float64()
			log.Printf("Setting brightness to %f!", brightness)
			light.setBrightness(brightness)
		default:
			log.Printf("Brightness got an unknown method %s", method)
		}

	})
	light.brightnessBus = brightnessBus

	colorBus, _ := light.Bus.AnnounceChannel("color", "color", methods, events, func(method string, payload *simplejson.Json) {
		log.Printf("Received actuation to change color, payload is: %s", payload)
		mode, _ := payload.Get("mode").String()
		log.Printf("Unsupported mode %s", mode)
		switch method {
		case "set":
			log.Printf("Setting color to %s!", payload)
			light.setColor(payload, method)
		default:
			log.Printf("Color got an unknown method %s", method)
		}
	})
	light.colorBus = colorBus

	return light, nil
}

func (l Light) turnOnOff(state bool) {
	l.refreshLightState()
	l.LightState.On = &state
	l.User.SetLightState(l.Id, l.LightState)
}

func (l Light) setBrightness(fbrightness float64) {
	brightness := uint8(fbrightness * math.MaxUint8)
	on := bool(true)
	l.LightState.Brightness = &brightness
	l.LightState.On = &on
	log.Printf("Setting brightness, fbrightness: %f, brightness: %d", fbrightness, brightness)
	l.refreshLightState()
	l.User.SetLightState(l.Id, l.LightState)

}

func (l Light) setColor(payload *simplejson.Json, mode string) {
	switch mode {
	case "hue": //less verbose plz
		fhue, _ := payload.Get("hue").Float64()
		hue := uint16(fhue * math.MaxUint16)
		fsaturation, _ := payload.Get("saturation").Float64()
		saturation := uint8(fsaturation * math.MaxUint8)
		on := bool(true)
		l.LightState.Hue = &hue
		l.LightState.Saturation = &saturation
		l.LightState.On = &on
		log.Printf("Color from setColor, fhue: %f, fsaturation: %f, hue: %d, saturation: %d", fhue, fsaturation, hue, saturation)
	case "xy":
		x, _ := payload.Get("x").Float64()
		y, _ := payload.Get("y").Float64()
		xy := []float64{x, y}
		l.LightState.XY = xy
	case "temperature":
		temp, _ := payload.Get("temperature").Float64()
		utemp := uint16(math.Floor(1000000 / temp))
		l.LightState.ColorTemp = &utemp
	default:
		log.Printf("Bad color mode: %s", mode)
	}
	l.refreshLightState()
	l.User.SetLightState(l.Id, l.LightState)

}

func createLightState() *hue.LightState {

	on := bool(false)
	brightness := uint8(0)
	saturation := uint8(0)
	hueVal := uint16(0)
	transitionTime := uint16(0)
	alert := ""
	temp := uint16(0)
	xy := []float64{0, 0}

	lightState := &hue.LightState{
		On:             &on,
		Brightness:     &brightness,
		Saturation:     &saturation,
		Hue:            &hueVal,
		ColorTemp:      &temp,
		TransitionTime: &transitionTime,
		Alert:          alert,
		XY:             xy,
	}

	return lightState
}

func getCurDir() string {
	pwd, err := os.Getwd()
	check(err)
	return pwd + "/"
}

func check(e error) {
	log.Printf("boom")
	if e != nil {
		panic(e)
	}
}

func (l Light) refreshLightState() {
	newstate, _ := l.User.GetLightAttributes(l.Id)
	bulbstate := newstate.State
	mybulbstate := l.LightState
	transtime := uint16(5) //TODO: Remove when web is sending sensible value
	// l.LightState = newstate.State //TODO why doesnt this work?

	mybulbstate.On = bulbstate.On
	mybulbstate.Brightness = bulbstate.Brightness
	mybulbstate.Hue = bulbstate.Hue
	mybulbstate.Saturation = bulbstate.Saturation
	mybulbstate.XY = bulbstate.XY
	mybulbstate.ColorTemp = bulbstate.ColorTemp
	mybulbstate.Alert = bulbstate.Alert
	mybulbstate.Effect = bulbstate.Effect
	mybulbstate.TransitionTime = &transtime
}

func printState(s *hue.LightState) {
	log.Printf(" on:%t brightness: %d hue: %d sat: %d x,y: %f, %f colortemp: %d alert: %s effect: %s color mode: %s reachable: %t", *s.On, *s.Brightness, *s.Hue, *s.Saturation, s.XY[0], s.XY[1], *s.ColorTemp, s.Alert, s.Effect, s.ColorMode, s.Reachable)

}

func main() {

	conn, err := ninja.Connect("10.0.1.171", 1883, "com.ninjablocks.hue") //TODO variable mqtt host and ID

	bus, err := conn.AnnounceDriver("com.ninjablocks.hue", "driver-hue", getCurDir())
	if err != nil {
		log.Fatalf("Could not get driver bus: %s", err)
	}

	bridge := getBridge()
	user := getUser(bridge)

	allLights, err := user.GetLights()
	if err != nil {
		log.Fatalf("Couldn't get lights:  %s", err)
	}

	for _, l := range allLights {
		_, err := NewLight(&l, bus, bridge, user)
		if err != nil {
			log.Fatalf("Error creating light instance:  %s", err)
		}

	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill)

	// Block until a signal is received.
	s := <-c
	fmt.Println("Got signal:", s)

}
