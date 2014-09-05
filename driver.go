package main

import (
	"fmt"
	"math"

	"github.com/bcurren/go-hue"
	"github.com/bitly/go-simplejson"
	"github.com/davecgh/go-spew/spew"
	"github.com/ninjasphere/go-ninja"
	"github.com/ninjasphere/go-ninja/channels"
	"github.com/ninjasphere/go-ninja/devices"
)

func NewLight(l *hue.Light, bus *ninja.DriverBus, bridge *hue.Bridge, user *hue.User) (*HueLightContext, error) {

	log.Infof("Making hue light with Bridge: %s Id: %+v", bridge.UniqueId, l.Id)

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
		Light:      light,
		LightState: &hue.LightState{},
	}

	light.ApplyOnOff = hl.ApplyOnOff
	light.ApplyLightState = hl.ApplyLightState

	return hl, nil
}

type HueLightContext struct {
	ID         string
	Name       string
	Bridge     *hue.Bridge
	User       *hue.User
	Light      *devices.LightDevice
	LightState *hue.LightState
}

func (hl *HueLightContext) ApplyOnOff(state bool) error {

	ls := createLightState()

	ls.On = &state

	return hl.SetLightState(hl.ID, ls)
}

func (hl *HueLightContext) ApplyLightState(state *devices.LightDeviceState) error {

	log.Debugf(spew.Sprintf("Sending light state to hue bulb: %+v", state))

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
			ls := createLightState()

			ls.Hue = getHue(state)
			ls.Saturation = getSaturation(state)
			ls.Brightness = getBrightness(state)
			ls.TransitionTime = getTransitionTime(state)

			return hl.SetLightState(hl.ID, ls)
		case "xy":

			ls := createLightState()

			ls.Hue = getHue(state)
			ls.Saturation = getSaturation(state)
			ls.Brightness = getBrightness(state)
			ls.XY = []float64{*state.Color.X, *state.Color.Y}
			ls.TransitionTime = getTransitionTime(state)

			return hl.SetLightState(hl.ID, ls)
		case "temperature":

			ls := createLightState()
			ls.ColorTemp = getColorTemp(state)
			ls.TransitionTime = getTransitionTime(state)

			return hl.SetLightState(hl.ID, ls)
		default:
			return fmt.Errorf("Unknown color mode %s", state.Color.Mode)
		}

	}

	return nil
}

func (hl *HueLightContext) SetLightState(lightID string, lightState *hue.LightState) error {

	log.Debugf(spew.Sprintf("Sending light state to hue bulb: %s %+v", lightID, lightState))

	if err := hl.User.SetLightState(hl.ID, lightState); err != nil {
		return err
	}

	//la, _ := hl.User.GetLightAttributes(hl.ID)

	//hl.Light.SetLightState(hueToNinjaLightState(la.State))

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

func hueToNinjaLightState(huestate *hue.LightState) *devices.LightDeviceState {

	onOff := *huestate.On
	brightness := float64(*huestate.Brightness) / float64(math.MaxUint16)
	hue := float64(*huestate.Hue) / float64(math.MaxUint16)
	saturation := float64(*huestate.Saturation) / float64(math.MaxUint16)

	return &devices.LightDeviceState{
		Color: &channels.ColorState{
			Mode:       "hue",
			Hue:        &hue,
			Saturation: &saturation,
		},
		Brightness: &brightness,
		OnOff:      &onOff,
	}
}
