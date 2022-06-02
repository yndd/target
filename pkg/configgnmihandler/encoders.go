/*
Copyright 2021 NDDO.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package configgnmihandler

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
