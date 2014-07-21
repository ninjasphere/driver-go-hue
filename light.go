package light

import(
  "github.com/bcurren/go-hue"
)

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
