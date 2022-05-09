package gnmiserver

import (
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/ygot/ygot"
)

type Encoder interface {
	Encode(nodeStruct ygot.ValidatedGoStruct) (map[string]interface{}, error)
	BuildUpdate(path *gnmi.Path, b []byte) *gnmi.Update
}

type JSONEncoder struct{}

func (e *JSONEncoder) Encode(nodeStruct ygot.ValidatedGoStruct) (map[string]interface{}, error) {
	return ygot.ConstructIETFJSON(nodeStruct, &ygot.RFC7951JSONConfig{})
}

func (e *JSONEncoder) BuildUpdate(path *gnmi.Path, b []byte) *gnmi.Update {
	return &gnmi.Update{Path: path, Val: &gnmi.TypedValue{Value: &gnmi.TypedValue_JsonVal{JsonVal: b}}}
}

type JSONIETFEncoder struct{}

func (e *JSONIETFEncoder) Encode(nodeStruct ygot.ValidatedGoStruct) (map[string]interface{}, error) {
	return ygot.ConstructIETFJSON(nodeStruct, &ygot.RFC7951JSONConfig{AppendModuleName: true})
}
func (e *JSONIETFEncoder) BuildUpdate(path *gnmi.Path, b []byte) *gnmi.Update {
	return &gnmi.Update{Path: path, Val: &gnmi.TypedValue{Value: &gnmi.TypedValue_JsonIetfVal{JsonIetfVal: b}}}
}
