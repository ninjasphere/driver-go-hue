package main

import (
	"fmt"
	"math"

	"github.com/bcurren/go-hue"
	"github.com/bitly/go-simplejson"
	"github.com/ninjasphere/go-ninja"
	"github.com/ninjasphere/go-ninja/channels"
	"github.com/ninjasphere/go-ninja/devices"
)

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
		Light:      light,
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
	Light      *devices.LightDevice
	LightState *hue.LightState
}

func (hl *HueLightContext) ApplyOnOff(state bool) error {
	lightState := &hue.LightState{
		On: &state,
	}
	return hl.SetLightState(hl.ID, lightState)
}

func (hl *HueLightContext) ApplyLightState(state *devices.LightDeviceState) error {

	log.Debugf("Sending light state to hue bulb: %+v", state)

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
			return hl.SetLightState(hl.ID, &hue.LightState{
				Hue:            getHue(state),
				Saturation:     getSaturation(state),
				Brightness:     getBrightness(state),
				TransitionTime: getTransitionTime(state),
			})
		case "xy":
			return hl.SetLightState(hl.ID, &hue.LightState{
				XY:             []float64{*state.Color.X, *state.Color.Y},
				TransitionTime: getTransitionTime(state),
			})
		case "temperature":
			return hl.SetLightState(hl.ID, &hue.LightState{
				ColorTemp:      getColorTemp(state),
				TransitionTime: getTransitionTime(state),
			})
		default:
			return fmt.Errorf("Unknown color mode %s", state.Color.Mode)
		}

	}

	return nil
}

func (hl *HueLightContext) SetLightState(lightID string, lightState *hue.LightState) error {

	if err := hl.User.SetLightState(hl.ID, lightState); err != nil {
		return err
	}

	la, _ := hl.User.GetLightAttributes(hl.ID)

	hl.Light.SetLightState(hueToNinjaLightState(la.State))

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
