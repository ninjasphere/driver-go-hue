package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/bcurren/go-hue"
	"github.com/bitly/go-simplejson"
	"github.com/davecgh/go-spew/spew"
	"github.com/ninjasphere/go-ninja"
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
	Batch         bool
	batchBus      *ninja.ChannelBus
	//bulbMonitor   *bulbmonitor.BulbMonitor
}

//--------------------------------------------------[HUE BASESTATION]----------------------------------------------------------------------

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
	serial, err := ninja.GetSerial()
	if err != nil {
		log.Fatalf("Problem determining if hue user is valid: %s", err)
	}
	username := serial + serial //username must be long 10-40 characters
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

//--------------------------------------------------[BUSES]----------------------------------------------------------------------

func getOnOffBus(light *Light) *ninja.ChannelBus {
	methods := []string{"turnOn", "turnOff", "set"}
	events := []string{"state"}
	onOffBus, _ := light.Bus.AnnounceChannel("on-off", "on-off", methods, events, func(method string, payload *simplejson.Json) {
		if light.Batch == true {
			return
		}
		switch method {
		case "turnOn":
			light.sendLightState(light.turnOnOff(true))
		case "turnOff":
			light.sendLightState(light.turnOnOff(false))
		case "set":
			state, _ := payload.GetIndex(0).Bool()
			light.sendLightState(light.turnOnOff(state))
		default:
			log.Printf("On-off got an unknown method %s", method)
			return
		}
	})

	return onOffBus
}

func getBrightBus(light *Light) *ninja.ChannelBus {
	methods := []string{"set"}
	events := []string{"state"}
	brightnessBus, _ := light.Bus.AnnounceChannel("brightness", "brightness", methods, events, func(method string, payload *simplejson.Json) {
		if light.Batch == true {
			return
		}

		switch method {
		case "set":
			brightness, _ := payload.GetIndex(0).Float64()
			log.Printf("Setting brightness to %f!", brightness)
			light.sendLightState(light.setBrightness(brightness))
		default:
			log.Printf("Brightness got an unknown method %s", method)
			return
		}

	})

	return brightnessBus

}

func getColorBus(light *Light) *ninja.ChannelBus {
	methods := []string{"set"}
	events := []string{"state"}
	colorBus, _ := light.Bus.AnnounceChannel("color", "color", methods, events, func(method string, payload *simplejson.Json) {
		if light.Batch == true {
			return
		}
		switch method {
		case "set":
			log.Printf("Setting color to %s!", payload)
			light.sendLightState(light.setColor(payload))
		default:
			log.Printf("Color got an unknown method %s", method)
		}
	})

	return colorBus
}

func getBatchBus(light *Light) *ninja.ChannelBus {
	methods := []string{"setBatch"}
	events := []string{"state"}
	batchBus, _ := light.Bus.AnnounceChannel("core.batching", "core.batching", methods, events, func(method string, payload *simplejson.Json) {
		switch method {
		case "setBatch":
			light.setBatchColor(payload.GetIndex(0))
		default:
			log.Printf("Color got an unknown method %s", method)
			return
		}
	})

	return batchBus
}

func NewLight(l *hue.Light, bus *ninja.DriverBus, bridge *hue.Bridge, user *hue.User) (*Light, error) { //TODO cut this down!

	lightState := createLightState()

	light := &Light{
		Id:         l.Id,
		Name:       l.Name,
		Bridge:     bridge,
		User:       user,
		LightState: lightState,
		Batch:      false,
	}

	sigs, _ := simplejson.NewJson([]byte(`{
			"ninja:manufacturer": "Phillips",
			"ninja:productName": "Hue",
			"manufacturer:productModelId": "",
			"ninja:productType": "Light",
			"ninja:thingType": "light"
	}`))

	la, _ := user.GetLightAttributes(l.Id)
	sigs.Set("manufacturer:productModelId", la.ModelId)

	deviceBus, _ := bus.AnnounceDevice(l.Id, "hue", l.Name, sigs)
	light.Bus = deviceBus
	light.OnOffBus = getOnOffBus(light)
	light.brightnessBus = getBrightBus(light)
	light.colorBus = getColorBus(light)
	light.batchBus = getBatchBus(light)

	return light, nil
}

func (l Light) turnOnOff(state bool) *hue.LightState {
	lightState := &hue.LightState{
		On: &state,
	}
	return lightState

}

func (l Light) setBrightness(fbrightness float64) *hue.LightState {
	brightness := uint8(fbrightness * math.MaxUint8)
	lightState := &hue.LightState{
		Brightness: &brightness,
	}
	return lightState
}

func (l Light) setColor(payload *simplejson.Json) *hue.LightState {

	var lightState *hue.LightState
	colorpayload := payload.Get("color")
	mode, err := colorpayload.Get("mode").String()
	if err != nil {
		log.Printf("No mode sent to color bus: %s", err)
		spew.Dump(payload)
	}

	switch mode {
	case "hue": //TODO less verbose plz
		fhue, err := colorpayload.Get("hue").Float64()
		if err != nil {
			log.Printf("No hue sent to color bus: %s", err)
			spew.Dump(payload)
		}
		uint16hue := uint16(fhue * math.MaxUint16)
		fsaturation, err := colorpayload.Get("saturation").Float64()
		if err != nil {
			log.Printf("No saturation sent to color bus: %s", err)
			spew.Dump(payload)
		}
		saturation := uint8(fsaturation * math.MaxUint8)
		lightState = &hue.LightState{
			Hue:        &uint16hue,
			Saturation: &saturation,
		}

	case "xy":
		x, err := colorpayload.Get("x").Float64()
		if err != nil {
			log.Printf("X coord not sent to color bus: %s", err)
			spew.Dump(payload)
		}
		y, err := colorpayload.Get("y").Float64()
		if err != nil {
			log.Printf("Y coord not sent to color bus: %s", err)
			spew.Dump(payload)
		}
		xy := []float64{x, y}
		lightState = &hue.LightState{
			XY: xy,
		}
	case "temperature":
		temp, err := colorpayload.Get("temperature").Float64()
		if err != nil {
			log.Printf("No temperature sent to color bus: %s", err)
			spew.Dump(payload)
		}
		utemp := uint16(math.Floor(1000000 / temp))
		lightState = &hue.LightState{
			ColorTemp: &utemp,
		}

	default:
		log.Printf("Bad color mode: %s", mode)
		return nil
	}

	if trans, e := payload.Get("transition").Int(); e == nil {
		st := l.setTransition(trans)
		lightState.TransitionTime = st.TransitionTime
	}

	return lightState

}

func (l Light) setTransition(transTime int) *hue.LightState {
	transTime = transTime / 10 //HUE API uses 1/10th of a second
	utranstime := uint16(transTime)
	newstate := &hue.LightState{
		TransitionTime: &utranstime,
	}
	return newstate
}

func (l Light) setBatchColor(payload *simplejson.Json) {
	var newlightstate *hue.LightState

	color := payload.Get("color")
	if color != nil {
		newlightstate = l.setColor(payload)
	}

	if brightness, err := payload.Get("brightness").Float64(); err == nil {
		bri := l.setBrightness(brightness)
		newlightstate.Brightness = bri.Brightness
	}

	if onoff, err := payload.Get("on-off").Bool(); err == nil {
		st := l.turnOnOff(onoff)
		newlightstate.On = st.On
	}

	if transition, err := payload.Get("transition").Int(); err == nil {
		trans := l.setTransition(transition)
		newlightstate.TransitionTime = trans.TransitionTime
	}

	l.sendLightState(newlightstate)

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
	pwd, _ := os.Getwd()
	return pwd + "/"
}

func (l Light) sendLightState(newstate *hue.LightState) {
	l.User.SetLightState(l.Id, newstate)
	l.OnOffBus.SendEvent("state", l.GetJsonLightState())
}

func (l Light) refreshLightState() { //TOOD fix verboseness
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

//Returns json state as defined by Ninja light protocol
func (l Light) GetJsonLightState() *simplejson.Json {
	st := l.LightState
	js := simplejson.New()
	js.Set("on", st.On)
	js.Set("bri", st.Brightness)
	js.Set("sat", st.Saturation)
	js.Set("hue", st.Hue)
	js.Set("ct", st.ColorTemp)
	js.Set("transitionTime", st.TransitionTime)
	js.Set("alert", st.Alert)
	js.Set("xy", st.XY)
	return js
}

func printState(s *hue.LightState) {
	spew.Dump(s)
}

func main() {

	conn, err := ninja.Connect("com.ninjablocks.hue")
	if err != nil {
		log.Fatalf("Could not connect to MQTT: %s", err)
	}

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

// func verifyState (sent *simpleJson) bool { TODO
//if sent state == current state, return true
//else return false
// }
