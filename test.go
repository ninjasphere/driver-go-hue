package main

import (
	"fmt"
	"github.com/bcurren/go-hue"
	"log"
	"reflect"
)

func createLightState() *hue.LightState {

	on := bool(false)
	brightness := uint8(0)
	saturation := uint8(0)
	hueVal := uint16(0)
	transitionTime := uint16(0)
	alert := ""
	temp := uint16(0)
	// xy := []float64{0, 0}

	lightState := &hue.LightState{
		On:             &on,
		Brightness:     &brightness,
		Saturation:     &saturation,
		Hue:            &hueVal,
		ColorTemp:      &temp,
		TransitionTime: &transitionTime,
		Alert:          alert,
		XY:             nil,
	}

	return lightState
}

func main() {

	t := createLightState()
	log.Printf("%+v", t)

	s := reflect.ValueOf(t).Elem()
	typeOfT := s.Type()
	for i := 0; i < s.NumField(); i++ {
		f := s.Field(i)
		fmt.Printf("%d: %s %s = %#v\n", i, typeOfT.Field(i).Name, f.Type(), f.Interface())
	}

}
