package ninja

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	MQTT "git.eclipse.org/gitroot/paho/org.eclipse.paho.mqtt.golang.git"
	"github.com/bitly/go-simplejson"
	"io/ioutil"
	"log"
	"time"
)

func Connect(host string, port int, clientId string) (*NinjaConnection, error) {
	mqttServer := fmt.Sprintf("tcp://%s:%d", host, port)
	conn := NinjaConnection{}
	opts := MQTT.NewClientOptions().SetBroker(mqttServer).SetClientId(clientId).SetCleanSession(true).SetTraceLevel(MQTT.Off)
	conn.mqtt = MQTT.NewClient(opts)
	_, err := conn.mqtt.Start()
	if err != nil {
		log.Fatalf("Failed to connect to mqtt server %s - %s", host, err)
	} else {
		log.Printf("Connected to %s\n", host)
	}
	return &conn, nil
}

type NinjaConnection struct {
	mqtt *MQTT.MqttClient
}

func (n NinjaConnection) AnnounceDriver(id string, name string, path string) (*DriverBus, error) {
	js, err := simplejson.NewJson([]byte(`{
		"params": [
		{
	    "name": "",
	    "file": "",
			"defaultConfig" : {},
			"package": {}
		}],
		"time":"",
		"jsonrpc":"2.0"
  }`))

	if err != nil {
		log.Fatalf("Bad json: %s", err)
	}

	pkginfo := getDriverInfo(path + "package.json")
	filename, err := pkginfo.Get("main").String()
	if err != nil {
		log.Fatalf("Couldn't retrieve main filename: %s", err)
	}

	mainfile := path + filename
	js.Get("params").GetIndex(0).Set("file", mainfile)
	js.Get("params").GetIndex(0).Set("name", id)
	js.Get("params").GetIndex(0).Set("package", pkginfo)
	js.Get("params").GetIndex(0).Set("defaultConfig", "{}") //TODO fill me out
	js.Set("time", time.Now().Unix())
	json, _ := js.MarshalJSON()

	serial := "TEST" //TODO: get config
	version, err := pkginfo.Get("version").String()
	if err != nil {
		log.Fatalf("No version available for driver %s: %s", id, err)
	}

	receipt := n.mqtt.Publish(MQTT.QoS(1), "$node/"+serial+"/app/"+id+"/event/announce", json)
	<-receipt

	driverBus := &DriverBus{
		id:      id,
		name:    name,
		mqtt:    n.mqtt,
		version: version,
	}

	return driverBus, nil
}

type DriverBus struct {
	id      string
	name    string
	version string
	mqtt    *MQTT.MqttClient
}

func (d DriverBus) AnnounceDevice(id string, idType string, name string, sigs *simplejson.Json) (*DeviceBus, error) {
	js, err := simplejson.NewJson([]byte(`{
    "params": [
        {
            "guid": "",
            "id": "",
            "idType": "",
            "name": "",
            "signatures": {},
            "driver": {
                "name": "",
                "version": ""
            }
        }
    ],
    "time": "",
    "jsonrpc": "2.0"
}`))

	if err != nil {
		log.Fatalf("Bad driver announce JSON: %s", js)
	}

	guid := GetGUID(d.id + id)
	js.Get("params").GetIndex(0).Set("guid", guid)
	js.Get("params").GetIndex(0).Set("id", id) //TODO patch driver to get MAC ID, rather than numberical ID
	js.Get("params").GetIndex(0).Set("idType", idType)
	js.Get("params").GetIndex(0).Set("name", name)
	js.Get("params").GetIndex(0).Set("signatures", sigs)
	js.Get("params").GetIndex(0).Get("driver").Set("name", d.name)
	js.Get("params").GetIndex(0).Get("driver").Set("version", d.version)
	js.Set("time", time.Now().Unix())

	json, err := js.MarshalJSON()
	if err != nil {
		log.Fatalf("Couldn't stringify: %s", err)
	}

	receipt := d.mqtt.Publish(MQTT.QoS(1), "$device/"+guid+"/announce/", json)
	<-receipt

	deviceBus := &DeviceBus{
		id:         id,
		idType:     idType,
		name:       name,
		driver:     &d,
		devicejson: js.Get("params").GetIndex(0),
	}

	return deviceBus, nil
}

type DeviceBus struct {
	id         string
	idType     string
	name       string
	driver     *DriverBus
	devicejson *simplejson.Json
}

type JsonMessageHandler func(string, *simplejson.Json)

func (d DeviceBus) AnnounceChannel(name string, protocol string, methods []string, events []string, serviceCallback JsonMessageHandler) (*ChannelBus, error) {
	deviceguid, _ := d.devicejson.Get("guid").String()
	channelguid := GetGUID(name + protocol)
	js, _ := simplejson.NewJson([]byte(`{
    "params": [
        	{
            "channel": "",
            "supported": {
                "methods": [],
                "events": []
            },
            "device": {}
        }
    ],
    "time": "",
    "jsonrpc": "2.0"
}`))

	js.Get("params").GetIndex(0).Set("device", d.devicejson)
	methodsjson := strArrayToJson(methods)
	js.Get("params").GetIndex(0).Get("supported").Set("methods", methodsjson)
	eventsjson := strArrayToJson(events)
	js.Get("params").GetIndex(0).Get("supported").Set("events", eventsjson)
	js.Get("params").GetIndex(0).Set("channel", name)
	js.Set("time", time.Now().Unix())

	json, err := js.MarshalJSON()

	if err != nil {
		log.Fatalf("Couldn't stringify that message %s", err)
	}

	topicBase := "$device/" + deviceguid + "/channel/" + channelguid + "/" + protocol

	pubReceipt := d.driver.mqtt.Publish(MQTT.QoS(0), topicBase+"/announce", json)
	<-pubReceipt

	log.Printf("Subscribing to : %s", topicBase)
	filter, err := MQTT.NewTopicFilter(topicBase, 0)
	if err != nil {
		log.Fatalf("unable to subscribe to %s in announcechannel: %s", topicBase, err)
	}
	_, err = d.driver.mqtt.StartSubscription(func(client *MQTT.MqttClient, message MQTT.Message) {
		json, _ := simplejson.NewJson(message.Payload())
		method, _ := json.Get("method").String()
		params := json.Get("params")

		log.Printf("incoming message! %s", params)
		serviceCallback(method, params)

	}, filter)

	if err != nil {
		log.Fatal(err)
	}

	channelBus := &ChannelBus{
		name:     name,
		protocol: protocol,
		device:   &d,
	}

	return channelBus, nil
}

func (cb ChannelBus) SendEvent(event string, payload *simplejson.Json) error {
	json, err := payload.MarshalJSON()
	if err != nil {
		return err
	}

	receipt := cb.device.driver.mqtt.Publish(MQTT.QoS(0), "$driver/"+cb.device.driver.id+"/device/"+cb.device.id+"/channel/"+cb.name+"/"+cb.protocol+"/event/"+event, json)
	<-receipt

	return nil
}

type ChannelBus struct {
	name     string
	protocol string
	device   *DeviceBus
	channel  <-chan MQTT.Receipt
}

func GetConnection() (*NinjaConnection, error) {
	host := "localhost"
	port := 1883
	clientId := "mqttClientId"

	return Connect(host, port, clientId)
}

func getDriverInfo(filename string) (res *simplejson.Json) {
	dat, err := ioutil.ReadFile(filename)
	check(err)
	js, err := simplejson.NewJson(dat)
	check(err)
	js.Del("scripts")
	log.Printf("driver info: %s", res)
	return js
}

func check(e error) {
	if e != nil {
		panic(e)
	}
}

func GetGUID(in string) string {

	h := md5.New()
	h.Write([]byte(in))
	str := hex.EncodeToString(h.Sum(nil))
	return str[:10]

}

func strArrayToJson(in []string) *simplejson.Json {
	str := "[ "
	for i, item := range in {
		if i < (len(in) - 1) { //commas between elements except for last item
			str += "\"" + item + "\", "
		} else {
			str += "\"" + item + "\" ]"
		}
	}

	out, err := simplejson.NewJson([]byte(str))
	if err != nil {
		log.Fatalf("Bad JSON in strArrayToJson %+v: %s", in, err)
	}

	return out
}
