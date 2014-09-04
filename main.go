package main

import (
	"fmt"
	"math"
	"os"
	"os/signal"

	"github.com/bcurren/go-hue"
	"github.com/bitly/go-simplejson"
	"github.com/ninjasphere/go-ninja"
	"github.com/ninjasphere/go-ninja/devices"
	"github.com/ninjasphere/go-ninja/logger"
)

const driverName = "driver-hue"

var log = logger.GetLogger(driverName)

func NewLight(l *hue.Light, bus *ninja.DriverBus, bridge *hue.Bridge, user *hue.User) (*devices.LightDevice, error) {

	log.Infof("Making hue light with Bridge: %s Id: %s", bridge.UniqueId, l.Id)

	sigs, _ := simplejson.NewJson([]byte(`{
			"ninja:manufacturer": "Phillips",
			"ninja:productName": "Hue",
			"manufacturer:productModelId": "",
			"ninja:productType": "Light",
			"ninja:thingType": "light"
	}`))

	la, _ := user.GetLightAttributes(l.Id)

	sigs.Set("manufacturer:productModelId", la.ModelId)

	deviceBus, err := bus.AnnounceDevice(l.Id, "hue", l.Name, sigs)

	if err != nil {
		log.FatalError(err, "Failed to create light device bus ")
	}

	light, err := devices.CreateLightDevice(l.Id, deviceBus)

	if err != nil {
		log.FatalError(err, "Failed to create light device")
	}

	if err := light.EnableOnOffChannel(); err != nil {
		log.FatalError(err, "Could not enable hue on-off channel")
	}

	if err := light.EnableBrightnessChannel(); err != nil {
		log.FatalError(err, "Could not enable hue brightness channel")
	}

	if err := light.EnableColorChannel("temperature", "hue"); err != nil {
		log.FatalError(err, "Could not enable hue color channel")
	}

	if err := light.EnableTransitionChannel(); err != nil {
		log.FatalError(err, "Could not enable hue transition channel")
	}

	hl := &HueLightContext{
		ID:         l.Id,
		Name:       l.Name,
		Bridge:     bridge,
		User:       user,
		LightState: &hue.LightState{},
	}

	light.ApplyOnOff = hl.ApplyOnOff
	light.ApplyLightState = hl.ApplyLightState

	return light, nil
}

type HueLightContext struct {
	ID         string
	Name       string
	Bridge     *hue.Bridge
	User       *hue.User
	LightState *hue.LightState
}

func (hl *HueLightContext) ApplyOnOff(state bool) error {
	lightState := &hue.LightState{
		On: &state,
	}
	return hl.User.SetLightState(hl.ID, lightState)

}

func (hl *HueLightContext) ApplyLightState(state *devices.LightDeviceState) error {

	log.Debugf("Sending light state to lifx bulb: %+v", state)

	if state.OnOff != nil {
		err := hl.ApplyOnOff(*state.OnOff)
		if err != nil {
			return err
		}
	}

	if state.Color != nil || state.Brightness != nil || state.Transition != nil {
		if state.Color == nil {
			return fmt.Errorf("Color value missing from batch set")
		}

		if state.Brightness == nil {
			return fmt.Errorf("Brightness value missing from batch set")
		}

		if state.Transition == nil {
			return fmt.Errorf("Transition value missing from batch set")
		}

		switch state.Color.Mode {
		case "hue":
			return hl.User.SetLightState(hl.ID, &hue.LightState{
				Hue:            getHue(state),
				Saturation:     getSaturation(state),
				Brightness:     getBrightness(state),
				TransitionTime: getTransitionTime(state),
			})
		case "xy":
			return hl.User.SetLightState(hl.ID, &hue.LightState{
				XY:             []float64{*state.Color.X, *state.Color.Y},
				TransitionTime: getTransitionTime(state),
			})
		case "temperature":
			return hl.User.SetLightState(hl.ID, &hue.LightState{
				ColorTemp:      getColorTemp(state),
				TransitionTime: getTransitionTime(state),
			})
		default:
			return fmt.Errorf("Unknown color mode %s", state.Color.Mode)
		}

	}

	return nil
}

func getTransitionTime(state *devices.LightDeviceState) *uint16 {
	var transTime uint16
	if *state.Transition > 0 && *state.Transition < math.MaxUint16 {
		transTime = uint16(*state.Transition / 100) //HUE API uses 1/10th of a second
	} else {
		transTime = 0
	}
	return &transTime
}

func getHue(state *devices.LightDeviceState) *uint16 {
	hue := uint16(*state.Color.Hue * math.MaxUint16)
	return &hue
}

func getSaturation(state *devices.LightDeviceState) *uint8 {
	saturation := uint8(*state.Color.Saturation * math.MaxUint16)
	return &saturation
}

func getBrightness(state *devices.LightDeviceState) *uint8 {
	brightness := uint8(*state.Brightness * math.MaxUint16)
	return &brightness
}

func getColorTemp(state *devices.LightDeviceState) *uint16 {
	temp := uint16(*state.Color.Temperature)
	return &temp
}

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
