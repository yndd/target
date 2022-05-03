/*
Copyright 2021 NDD.

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

package gnmiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnmi/value"
	"github.com/openconfig/ygot/util"
	"github.com/openconfig/ygot/ygot"
	"github.com/openconfig/ygot/ytypes"
	"github.com/yndd/ndd-runtime/pkg/model"
	"github.com/yndd/ndd-target-runtime/pkg/cachename"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *GnmiServerImpl) Get(ctx context.Context, req *gnmi.GetRequest) (*gnmi.GetResponse, error) {
	ok := s.unaryRPCsem.TryAcquire(1)
	if !ok {
		return nil, status.Errorf(codes.ResourceExhausted, "max number of Unary RPC reached")
	}
	defer s.unaryRPCsem.Release(1)

	// We dont act upon the error here, but we pass it on the response with updates
	ns, err := s.HandleGet(req)
	return &gnmi.GetResponse{
		Notification: ns,
	}, err
}

func (s *GnmiServerImpl) HandleGet(req *gnmi.GetRequest) ([]*gnmi.Notification, error) {
	prefix := req.GetPrefix()
	log := s.log.WithValues("origin", prefix.GetOrigin(), "target", prefix.GetTarget())
	log.Debug("Get...", "path", req.GetPath())

	cacheNsTargetName := cachename.NamespacedName(prefix.GetTarget()).GetPrefixNamespacedName(prefix.GetOrigin())

	ce, err := s.cache.GetEntry(cacheNsTargetName)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "cache not ready")
	}

	goStruct := ce.GetRunningConfig() // no DeepCopy required, since we get a deepcopy already
	model := ce.GetModel()
	ts := time.Now().UnixNano()

	return populateNotification(goStruct, req, model, ts, prefix)
}

func populateNotification(goStruct ygot.GoStruct, req *gnmi.GetRequest, model *model.Model, ts int64, prefix *gnmi.Path) ([]*gnmi.Notification, error) {
	notifications := make([]*gnmi.Notification, len(req.GetPath()))
	// process all the paths from the given request
	for i, path := range req.GetPath() {
		fullPath := path
		if fullPath.GetElem() == nil && fullPath.GetElem() != nil {
			return nil, status.Error(codes.Unimplemented, "deprecated path element type is unsupported")
		}

		nodes, err := ytypes.GetNode(model.SchemaTreeRoot, goStruct, fullPath,
			&ytypes.GetPartialKeyMatch{},
			&ytypes.GetHandleWildcards{},
		)
		if len(nodes) == 0 || err != nil || util.IsValueNil(nodes[0].Data) {
			return nil, status.Errorf(codes.NotFound, "path %v not found: %v", fullPath, err)
		}

		// with wildcards allowed, we might get multiple entries per path query
		// hence the updates are stored in a slice.
		updates := []*gnmi.Update{}

		// create a new NOS specific ygot Device from the model contained pointer
		r := reflect.New(model.StructRootType.Elem())
		// take the new NOS specific ygot Device and create a ygot.GoStruct pointer from it
		// to work with in the following
		resultCollectorYgotDevice := ptr(r).Elem().Interface().(ygot.ValidatedGoStruct)

		ygotstructProcessed := false
		// generate updates for all the retrieved nodes
		for _, entry := range nodes {
			node := entry.Data
			nodeStruct, isYgotStruct := node.(ygot.GoStruct)

			// it seems like there is a chance we get nil structs,
			// these should be skipped
			// TODO: To me it seems like we need this due to a BUG. this is triggered through the tests, when querying for
			// for ipv4 in wildcard interface + wildcard subinterface... the ipv6 only subinterface will appear with a nil pointer.
			// Should be checked if this is expected or needs to be fixed in the ygot library.
			if reflect.ValueOf(entry.Data).Kind() == reflect.Ptr && reflect.ValueOf(entry.Data).IsNil() {
				continue
			}

			// if not ok, this mut be a leafnode, meaning a scalar value instead of any ygot struct
			if !isYgotStruct {
				update, err := createLeafNodeUpdate(node, fullPath, model)
				if err != nil {
					return nil, err
				}
				updates = append(updates, update)
			} else {
				// process ygot structs
				//update, err = createYgotStructNodeUpdate(nodeStruct, path, req.GetEncoding())
				ygotstructProcessed = true

				err := processYgotStruct(entry, nodeStruct, model, resultCollectorYgotDevice)
				if err != nil {
					return nil, err
				}
			}
		}
		// if the result contained a ygot.GoStruct and not just terminal values, we need to create the update
		// now out of the data collected within the resultCollectorYgotDevice
		if ygotstructProcessed {
			update, err := createYgotStructNodeUpdate(resultCollectorYgotDevice, &gnmi.Path{}, req.GetEncoding())
			if err != nil {
				return nil, err
			}
			updates = append(updates, update)
		}
		// generate the notification on a per path basis, as defined by the RFC
		notifications[i] = &gnmi.Notification{
			Timestamp: ts,
			Prefix:    prefix,
			Update:    updates,
		}
	}
	return notifications, nil
}

// processYgotStruct processes the ygot.GoStruct and combine it into the given resultCollectorYgotDevice
// with the purpose of merging the different results of a wildcard query into a single gnmi.update
func processYgotStruct(entry *ytypes.TreeNode, nodeStruct ygot.GoStruct, model *model.Model, resultCollectorYgotDevice ygot.GoStruct) error {
	// convert config to GnmiNotification
	notifications, err := ygot.TogNMINotifications(nodeStruct, 0, ygot.GNMINotificationsConfig{UsePathElem: true})
	if err != nil {
		return err
	}

	FirstPartPath := []*gnmi.PathElem{}
	if entry.Path != nil && entry.Path.Elem != nil {
		FirstPartPath = entry.Path.Elem
	}

	// iterate over the notifications and their enclosed updates, which contain all the leaf values and their paths
	// they will then one by one be added to the resultCollection ygotstruct
	for _, n := range notifications {
		for _, u := range n.GetUpdate() {
			// construct the path that the value needs to go into

			path := &gnmi.Path{Elem: append(FirstPartPath, u.Path.Elem...)}
			// add the single value to the resultCollection ygot device.
			err = ytypes.SetNode(model.SchemaTreeRoot, resultCollectorYgotDevice, path, u.Val, &ytypes.InitMissingElements{})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// ptr wraps the given value with pointer: V => *V, *V => **V, etc.
func ptr(v reflect.Value) reflect.Value {
	pt := reflect.PtrTo(v.Type()) // create a *T type.
	pv := reflect.New(pt.Elem())  // create a reflect.Value of type *T.
	pv.Elem().Set(v)              // sets pv to point to underlying value of v.
	return pv
}

// createLeafNodeUpdate processes the list of returned nodes from the cache, which are Leaf Nodes, carrying terminal values
// rather then ygot struct kind of data. From these, the resulting gnmi.update stucts are being populated.
func createLeafNodeUpdate(node interface{}, path *gnmi.Path, model *model.Model) (*gnmi.Update, error) {
	var val *gnmi.TypedValue
	switch kind := reflect.ValueOf(node).Kind(); kind {
	case reflect.Ptr, reflect.Interface:
		var err error
		val, err = value.FromScalar(reflect.ValueOf(node).Elem().Interface())
		if err != nil {
			msg := fmt.Sprintf("leaf node %v does not contain a scalar type value: %v", path, err)
			return nil, status.Error(codes.Internal, msg)
		}
	case reflect.Int64:
		enumMap, ok := model.EnumData[reflect.TypeOf(node).Name()]
		if !ok {
			return nil, status.Error(codes.Internal, "not a GoStruct enumeration type")
		}
		val = &gnmi.TypedValue{
			Value: &gnmi.TypedValue_StringVal{
				StringVal: enumMap[reflect.ValueOf(node).Int()].Name,
			},
		}
	default:
		return nil, status.Errorf(codes.Internal, "unexpected kind of leaf node type: %v %v", node, kind)
	}

	update := &gnmi.Update{Path: path, Val: val}
	return update, nil
}

// createYgotStructNodeUpdate generated update messages from ygot structs.
func createYgotStructNodeUpdate(nodeStruct ygot.ValidatedGoStruct, path *gnmi.Path, requestedEncoding gnmi.Encoding) (*gnmi.Update, error) {
	// take care of encoding
	// default to JSON IETF, other option is plain JSON
	var encoder Encoder
	switch requestedEncoding {
	case gnmi.Encoding_JSON:
		encoder = &JSONEncoder{}
	default:
		encoder = &JSONIETFEncoder{}
	}

	jsonTree, err := encoder.Encode(nodeStruct)
	if err != nil {
		msg := fmt.Sprintf("error in constructing JSON tree from requested node: %v", err)
		return nil, status.Error(codes.Internal, msg)
	}
	jsonDump, err := json.Marshal(jsonTree)
	if err != nil {
		msg := fmt.Sprintf("error in marshaling JSON tree to bytes: %v", err)
		return nil, status.Error(codes.Internal, msg)
	}

	return encoder.BuildUpdate(path, jsonDump), nil
}
