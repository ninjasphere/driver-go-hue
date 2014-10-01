package main

import (
	"fmt"
	"math"

	"github.com/davecgh/go-spew/spew"
	"github.com/ninjasphere/go-hue"
	"github.com/ninjasphere/go-ninja/api"
	"github.com/ninjasphere/go-ninja/channels"
	"github.com/ninjasphere/go-ninja/devices"
	"github.com/ninjasphere/go-ninja/logger"
	"github.com/ninjasphere/go-ninja/model"
)

var defaultTransitionTime uint16 = 7 // 1/10ths of a second

var info = ninja.LoadModuleInfo("./package.json")

type HueDriver struct {
	log       *logger.Logger
	config    *HueDriverConfig
	conn      *ninja.Connection
	bridge    *hue.Bridge
	user      *hue.User
	sendEvent func(event string, payload interface{}) error
}

func NewHueDriver() {
	d := &HueDriver{
		log: logger.GetLogger(info.Name),
	}

	conn, err := ninja.Connect(info.ID)
	d.conn = conn
	if err != nil {
		d.log.Fatalf("Failed to connect to MQTT: %s", err)
	}

	err = conn.ExportDriver(d)
	if err != nil {
		d.log.Fatalf("Failed to export driver: %s", err)
	}
}

type HueDriverConfig struct {
}

func (d *HueDriver) Start(config *HueDriverConfig) error {
	d.config = config

	d.bridge = getBridge()
	d.user = getUser(d.bridge)

	allLights, err := d.user.GetLights()
	if err != nil {
		d.log.HandleError(err, "Couldn't get lights")
		return err
	}

	for _, l := range allLights {
		_, err := d.newLight(&l)
		if err != nil {
			d.log.HandleError(err, "Error creating light instance")
		}

	}
	return nil
}

func (d *HueDriver) GetModuleInfo() *model.Module {
	return info
}

func (d *HueDriver) SetEventHandler(sendEvent func(event string, payload interface{}) error) {
	d.sendEvent = sendEvent
}

func (d *HueDriver) newLight(bulb *hue.Light) (*HueLightContext, error) { //TODO cut this down!

	name := bulb.Name

	d.log.Infof("Making light on Bridge: %s ID: %s Label: %s", d.bridge.UniqueId, bulb.Id, name)

	d.log.Infof("connection %s", d.conn)

	attrs, _ := d.user.GetLightAttributes(bulb.Id)

	light, err := devices.CreateLightDevice(d, &model.Device{
		NaturalID:     bulb.Id,
		NaturalIDType: "hue",
		Name:          &name,
		Signatures: &map[string]string{
			"ninja:manufacturer":          "Phillips",
			"ninja:productName":           "Hue",
			"ninja:productType":           "Light",
			"ninja:thingType":             "light",
			"manufacturer:productModelId": attrs.ModelId,
		},
	}, d.conn)

	if err != nil {
		d.log.FatalError(err, "Could not create light device")
	}
	//func NewLight(l *hue.Light, bus *ninja.DriverBus, bridge *hue.Bridge, user *hue.User) (*HueLightContext, error) {

	if err := light.EnableOnOffChannel(); err != nil {
		d.log.FatalError(err, "Could not enable hue on-off channel")
	}

	if err := light.EnableBrightnessChannel(); err != nil {
		d.log.FatalError(err, "Could not enable hue brightness channel")
	}

	if err := light.EnableColorChannel("temperature", "hue"); err != nil {
		d.log.FatalError(err, "Could not enable hue color channel")
	}

	if err := light.EnableTransitionChannel(); err != nil {
		d.log.FatalError(err, "Could not enable hue transition channel")
	}

	hl := &HueLightContext{
		ID:         bulb.Id,
		Name:       bulb.Name,
		Bridge:     d.bridge,
		User:       d.user,
		Light:      light,
		LightState: &hue.LightState{},
	}

	light.ApplyLightState = hl.ApplyLightState

	return hl, hl.UpdateState()
}

type HueLightContext struct {
	ID                 string
	Name               string
	Bridge             *hue.Bridge
	User               *hue.User
	Light              *devices.LightDevice
	LightState         *hue.LightState
	lastTransitionTime *uint16
	log                logger.Logger
}

func (hl *HueLightContext) ApplyLightState(state *devices.LightDeviceState) error {

	hl.log.Debugf(spew.Sprintf("Sending light state to hue bulb: %+v", state))

	ls := createLightState()

	// Hue doesn't like you setting anything if you're off
	if state.OnOff == nil || !*state.OnOff {
		ls := createLightState()
		on := false
		ls.On = &on
		return hl.SetLightState(ls)
	}

	ls.On = &*state.OnOff
	ls.Brightness = getBrightness(state)

	if state.Transition != nil {
		ls.TransitionTime = getTransitionTime(state)
	} else if hl.lastTransitionTime != nil {
		ls.TransitionTime = hl.lastTransitionTime
	} else {
		ls.TransitionTime = &defaultTransitionTime
	}

	if state.Color != nil || state.Brightness != nil || state.Transition != nil {
		if state.Color == nil {
			return fmt.Errorf("Color value missing from batch set")
		}

		if state.Brightness == nil {
			return fmt.Errorf("Brightness value missing from batch set")
		}

		// we have a default now
		/*if state.Transition == nil {
			return fmt.Errorf("Transition value missing from batch set")
		}*/
	}

	switch state.Color.Mode {
	case "hue":
		ls.Hue = getHue(state)
		ls.Saturation = getSaturation(state)

	case "xy":

		ls.XY = []float64{*state.Color.X, *state.Color.Y}

	case "temperature":

		ls.ColorTemp = getColorTemp(state)

	default:
		return fmt.Errorf("Unknown color mode %s", state.Color.Mode)
	}

	return hl.SetLightState(ls)
}

func (hl *HueLightContext) SetLightState(lightState *hue.LightState) error {

	hl.lastTransitionTime = lightState.TransitionTime

	hl.log.Debugf(spew.Sprintf("Sending light state to hue bulb: %s %+v", hl.ID, lightState))

	if err := hl.User.SetLightState(hl.ID, lightState); err != nil {
		return err
	}

	return hl.UpdateState()
}

func (hl *HueLightContext) UpdateState() error {

	hl.log.Debugf("Updating light state")

	la, err := hl.User.GetLightAttributes(hl.ID)
	if err != nil {
		return err
	}

	hl.Light.SetLightState(hl.toNinjaLightState(la.State))

	return nil
}

func (hl *HueLightContext) toNinjaLightState(huestate *hue.LightState) *devices.LightDeviceState {

	onOff := *huestate.On
	brightness := float64(*huestate.Brightness) / float64(math.MaxUint16)
	hue := float64(*huestate.Hue) / float64(math.MaxUint16)
	saturation := float64(*huestate.Saturation) / float64(math.MaxUint16)

	transition := int(defaultTransitionTime) * 100

	if hl.lastTransitionTime != nil {
		transition = int(*hl.lastTransitionTime) * 100
	}

	return &devices.LightDeviceState{
		Color: &channels.ColorState{
			Mode:       "hue",
			Hue:        &hue,
			Saturation: &saturation,
		},
		Brightness: &brightness,
		OnOff:      &onOff,
		Transition: &transition,
	}
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
	return &hue.LightState{}
}
