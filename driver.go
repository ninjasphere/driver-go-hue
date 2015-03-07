package main

import (
	"encoding/json"
	"errors"
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

var defaultTransitionTime = 500 // 500ms
var defaultBrightness = 1.0     // Full brightness
var xxx float64
var defaultColor = channels.ColorState{
	Mode:       "Hue",
	Hue:        &xxx,
	Saturation: &xxx,
}

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
	d.user = getUser(d, d.bridge)

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

	attrs, _ := d.user.GetLightAttributes(bulb.Id)

	d.log.Debugf("Found hue bulb %s - %s", dump(*bulb), dump(attrs))

	sigs := map[string]string{
		"ninja:productType": "Light",
		"ninja:thingType":   "light",
		"hue:bridge":        d.user.Bridge.UniqueId,
		"hue:modelId":       attrs.ModelId,
		"hue:type":          attrs.Type,
		"hue:swversion":     attrs.SoftwareVersion,
	}

	if attrs.ModelId != "ZLL Light" {
		sigs["ninja:manufacturer"] = "Phillips"
		sigs["ninja:productName"] = "Hue"
	}

	light, err := devices.CreateLightDevice(d, &model.Device{
		NaturalID:/* SHOULD HAVE BRIDGE ID IN HERE!! d.user.Bridge.UniqueId + "-" +*/ bulb.Id,
		NaturalIDType: "hue",
		Name:          &name,
		Signatures:    &sigs,
	}, d.conn)

	if err != nil {
		d.log.FatalError(err, "Could not create light device")
	}
	//func NewLight(l *hue.Light, bus *ninja.DriverBus, bridge *hue.Bridge, user *hue.User) (*HueLightContext, error) {

	hl := &HueLightContext{
		ID:     bulb.Id,
		Name:   bulb.Name,
		Bridge: d.bridge,
		User:   d.user,
		Light:  light,
		log:    logger.GetLogger(fmt.Sprintf("huelight:%s", bulb.Id)),
	}

	if err := light.EnableOnOffChannel(); err != nil {
		d.log.FatalError(err, "Could not enable hue on-off channel")
	}

	if err := light.EnableBrightnessChannel(); err != nil {
		d.log.FatalError(err, "Could not enable hue brightness channel")
	}

	if attrs.State.ColorMode != "" {
		if err := light.EnableColorChannel("temperature", "hue"); err != nil {
			d.log.FatalError(err, "Could not enable hue color channel")
		}
		hl.colorEnabled = true
	}

	if err := light.EnableTransitionChannel(); err != nil {
		d.log.FatalError(err, "Could not enable hue transition channel")
	}

	light.ApplyIdentify = hl.ApplyIdentify

	if err := light.EnableIdentifyChannel(); err != nil {
		d.log.FatalError(err, "Could not enable identify channel")
	}

	light.ApplyLightState = hl.ApplyLightState

	return hl, hl.updateState()
}

type HueLightContext struct {
	ID             string
	Name           string
	Bridge         *hue.Bridge
	User           *hue.User
	Light          *devices.LightDevice
	lastState      *devices.LightDeviceState // Used to fill an incomplete request, using the previous state
	desiredState   devices.LightDeviceState  // Used when the globe is updated while it is off
	lastTransition *int
	colorEnabled   bool
	log            *logger.Logger
}

func (hl *HueLightContext) ApplyIdentify() error {
	state := &hue.LightState{
		Alert: "lselect", // Toggles 10x
	}

	return hl.User.SetLightState(hl.ID, state)
}

func (hl *HueLightContext) ApplyLightState(state *devices.LightDeviceState) error {

	hl.log.Infof("Applying light state: %s", dump(*state))

	if state.OnOff == nil {

		if state.Color == nil && state.Brightness == nil {
			// We've been given nothing. Nothing to do!

			if state.Transition != nil {
				// We at least got a transition time... keep that for later
				hl.desiredState.Transition = state.Transition
			}
			return nil
		}

		// We have been color or brightness, but not an on-off state, assume they want it on.
		on := true
		state.OnOff = &on
	}

	if state.Transition == nil {
		if hl.desiredState.Transition != nil {
			// Use the transition previously asked for
			state.Transition = hl.desiredState.Transition
		} else if hl.lastState != nil && hl.lastState.Transition != nil {
			// Use the transition we last used
			state.Transition = hl.lastState.Transition
		} else {
			// Use the default
			state.Transition = &defaultTransitionTime
		}
	}

	hl.lastTransition = state.Transition

	// Hue doesn't like you setting anything if you're off
	if !*state.OnOff {

		// cache the desired state so we can send this later
		// note this is to get around the fact we can't send color to the hue bulb if it is OFF
		// but we need to keep these changes for when the user turns it on!
		if state.Brightness != nil {
			hl.desiredState.Brightness = state.Brightness
		}
		if state.Transition != nil {
			hl.desiredState.Transition = state.Transition
		}
		if state.Color != nil {
			hl.desiredState.Color = state.Color
		}

		// just set the on-off state and transition and pass it to the globe
		on := false
		return hl.setLightState(&hue.LightState{
			On:             &on,
			TransitionTime: getTransitionTime(state),
		})
	}

	if state.Brightness == nil {
		// We have no brightness, but hue requires one to be sent.

		if hl.desiredState.Brightness != nil {
			// Use the brightness previously asked for
			state.Brightness = hl.desiredState.Brightness
		} else if hl.lastState != nil && hl.lastState.Brightness != nil {
			// Use the previously seen brightness
			state.Brightness = hl.lastState.Brightness
		} else {
			// Use the default
			state.Brightness = &defaultBrightness
		}
	}

	if state.Color == nil {
		// We have no color, but hue requires one to be sent.

		if hl.desiredState.Color != nil {
			// Use the color previously asked for
			state.Color = hl.desiredState.Color
		} else if hl.lastState != nil && hl.lastState.Color != nil {
			// Use the color we last saw
			state.Color = hl.lastState.Color
		} else {
			// Set it to the default color
			state.Color = &defaultColor
		}
	}

	if state.OnOff == nil || state.Color == nil || state.Brightness == nil || state.Transition == nil {
		// This shouldn't happen... but just in case...
		return fmt.Errorf("Values missing from light state... %s", dump(state))
	}

	hl.log.Debugf("Applying state (after defaults + desired) : %s", dump(state))

	outgoingState := &hue.LightState{
		On:             &*state.OnOff, // Always true at this point
		Brightness:     getBrightness(state),
		TransitionTime: getTransitionTime(state),
	}

	if hl.colorEnabled {

		switch state.Color.Mode {
		case "hue":

			if state.Color.Hue == nil {
				return fmt.Errorf("Missing color hue")
			}

			if state.Color.Saturation == nil {
				return fmt.Errorf("Missing color saturation")
			}

			outgoingState.Hue = getHue(state)
			outgoingState.Saturation = getSaturation(state)

		case "xy":

			if state.Color.X == nil || state.Color.Y == nil {
				return fmt.Errorf("Missing X or Y from xy color state: %s", dump(state.Color))
			}

			outgoingState.XY = []float64{*state.Color.X, *state.Color.Y}

		case "temperature":

			if state.Color.Temperature == nil {
				return fmt.Errorf("Missing color temperature: %+v", *state.Color)
			}

			outgoingState.ColorTemp = getColorTemp(state)

			hl.log.Debugf("color temp: %d ", *outgoingState.ColorTemp)
		default:
			return fmt.Errorf("Unknown color mode %s", state.Color.Mode)
		}
	}

	return hl.setLightState(outgoingState)
}

func (hl *HueLightContext) setLightState(lightState *hue.LightState) error {

	hl.log.Debugf("Sending light state to hue bulb: %s", dump(lightState))

	if err := hl.User.SetLightState(hl.ID, lightState); err != nil {
		return err
	}

	return hl.updateState()
}

func (hl *HueLightContext) updateState() error {

	la, err := hl.User.GetLightAttributes(hl.ID)
	if err != nil {
		return err
	}

	state := hl.toNinjaLightState(la.State)

	// If the light is on, we can destroy our desired state.. as it has either been used,
	// or the user has set it to something else using another app.
	if *state.OnOff {
		hl.desiredState = devices.LightDeviceState{}
	}

	// if we have desired state... use that.
	if hl.desiredState.Color != nil {
		state.Color = hl.desiredState.Color
	}
	if hl.desiredState.Brightness != nil {
		state.Brightness = hl.desiredState.Brightness
	}
	if hl.desiredState.Transition != nil {
		state.Transition = hl.desiredState.Transition
	}

	hl.log.Debugf("Updating light state: %+v", state)
	hl.lastState = state

	hl.Light.SetLightState(state)

	return nil
}

// ES: Yes, it isn't great. Whatever. Go fix something else.
func dump(obj interface{}) string {
	x, err := json.Marshal(obj)
	if err != nil {
		return spew.Sprintf("%+v", obj)
	}
	return string(x)
}

func (hl *HueLightContext) toNinjaLightState(huestate *hue.LightState) *devices.LightDeviceState {

	hl.log.Debugf("Converting hue state to ninja %s", dump(huestate))

	onOff := *huestate.On

	transition := defaultTransitionTime

	if hl.lastTransition != nil {
		transition = *hl.lastTransition
	}

	brightness := float64(*huestate.Brightness) / float64(math.MaxUint8)

	hl.log.Infof("Converted brightness from %v to %v", *huestate.Brightness, brightness)

	lds := &devices.LightDeviceState{
		Brightness: &brightness,
		OnOff:      &onOff,
		Transition: &transition,
	}

	if hl.colorEnabled {

		switch huestate.ColorMode {
		case "ct":
			// see: http://en.wikipedia.org/wiki/Mired
			// t = 1000000 / m
			temp := float64(1000000 / int(*huestate.ColorTemp))

			lds.Color = &channels.ColorState{
				Mode:        "temperature",
				Temperature: &temp,
			}
		case "xy":
			lds.Color = &channels.ColorState{
				Mode: "xy",
				X:    &huestate.XY[0],
				Y:    &huestate.XY[1],
			}
		case "hs":
			hue := float64(*huestate.Hue) / float64(math.MaxUint16)
			saturation := float64(*huestate.Saturation) / float64(math.MaxUint16)

			lds.Color = &channels.ColorState{
				Mode:       "hue",
				Hue:        &hue,
				Saturation: &saturation,
			}
		default:
			hl.log.FatalError(errors.New("Invalid color mode"), "Failed to load hue state")
		}
	}

	hl.log.Infof("Converted to ninja state %s", dump(lds))

	return lds
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
	brightness := uint8(*state.Brightness * math.MaxUint8)
	return &brightness
}

func getColorTemp(state *devices.LightDeviceState) *uint16 {
	// see: http://en.wikipedia.org/wiki/Mired
	// m = 1000000 / t
	temp := uint16(1000000 / int(*state.Color.Temperature))
	return &temp
}
