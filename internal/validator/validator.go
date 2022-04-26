package validator

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnmi/value"
	"github.com/openconfig/goyang/pkg/yang"
	"github.com/openconfig/ygot/ygot"
	"github.com/openconfig/ygot/ytypes"
	"github.com/pkg/errors"
	"github.com/yndd/ndd-target-runtime/internal/cache"
	"github.com/yndd/ndd-yang/pkg/yparser"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Origin string

const (
	Origin_Subscription Origin = "subsription"
	Origin_Reconciler   Origin = "reconciler"
	Origin_GnmiServer   Origin = "gnmiserver"
	Origin_Target       Origin = "target"
)

func ValidateCreate(ce cache.CacheEntry, x interface{}) (ygot.ValidatedGoStruct, error) {
	m := ce.GetModel()
	curGoStruct := ce.GetRunningConfig()

	//fmt.Printf("ValidateCreate: target: %s data: %v\n", target, x)
	newConfig, err := json.Marshal(x)
	if err != nil {
		fmt.Printf("ValidateCreate: Marshal error %s\n", err.Error())
		return nil, err
	}

	//fmt.Printf("ValidateCreate: New Config %s\n", string(newConfig))

	newGoStruct, err := m.NewConfigStruct(newConfig, false)
	if err != nil {
		fmt.Printf("ValidateCreate: NewConfigStruct error %s\n", err.Error())
		return nil, err
	}

	// if the current gostruct is nil we can validate and return the newGoStruct
	if curGoStruct == nil {
		if err := newGoStruct.Validate(); err != nil {
			fmt.Printf("ValidateCreate: validation error %s\n", err.Error())
			return nil, err
		}
		return newGoStruct, nil
	}

	if err := ygot.MergeStructInto(curGoStruct, newGoStruct, &ygot.MergeOverwriteExistingFields{}); err != nil {
		fmt.Printf("ValidateCreate: MergeStructInto error %s\n", err.Error())
		return nil, err
	}
	/*
		j, err := ygot.EmitJSON(curGoStruct, &ygot.EmitJSONConfig{
			Format:         ygot.RFC7951,
			SkipValidation: true,
		})
		if err != nil {
			fmt.Printf("ValidateCreate: EmitJSON error %s\n", err.Error())
			return nil, err
		}
		fmt.Println("json newConfig start")
		fmt.Println(j)
		fmt.Println("json newConfig end")
	*/

	if err := curGoStruct.Validate(); err != nil {
		fmt.Printf("ValidateCreate: validation error %s\n", err.Error())
		return nil, err
	}
	//fmt.Printf("ValidateCreate: all good\n")
	return newGoStruct, nil
}

func ValidateUpdate(ce cache.CacheEntry, updates []*gnmi.Update, replace, jsonietf bool, origin Origin) error {
	curGoStruct := ce.GetRunningConfig()

	if curGoStruct == nil {
		return fmt.Errorf("ValidateUpdate origin: %s using empty go struct", origin)
	}
	//fmt.Printf("ValidateUpdate deviceName: %s, goStruct: %v\n", target, curGoStruct)
	jsonTree, err := ygot.ConstructIETFJSON(curGoStruct, &ygot.RFC7951JSONConfig{})
	if err != nil {
		return errors.Wrap(err, "error in constructing IETF JSON tree from config struct")
	}

	m := ce.GetModel()

	//fmt.Printf("ValidateUpdate deviceName: %s, curGoStruct: %v\n", target, curGoStruct)
	//fmt.Printf("ValidateUpdate deviceName: %s, jsonTree: %v\n", target, jsonTree)
	for _, u := range updates {
		fullPath := cleanPath(u.GetPath())
		val := u.GetVal()
		if origin == Origin_Subscription {
			fmt.Printf("ValidateUpdate origin: %s: path: %s, val: %v\n", origin, yparser.GnmiPath2XPath(fullPath, true), val)
		}
	}

	//var goStruct ygot.ValidatedGoStruct
	for _, u := range updates {
		fullPath := cleanPath(u.GetPath())
		val := u.GetVal()

		if origin == Origin_Subscription {
			fmt.Printf("ValidateUpdate origin: %s: path: %s, val: %v\n", origin, yparser.GnmiPath2XPath(fullPath, true), val)
		}

		//	if replace {
		//		fmt.Printf("ValidateUpdate path: %s, val: %v\n", yparser.GnmiPath2XPath(fullPath, true), val)
		//	}

		// we need to return to the schema root for the next update
		schema := m.SchemaTreeRoot

		// Validate the operation
		var emptyNode interface{}
		if replace {
			//fmt.Printf("ValidateUpdate val: %v\n", val)
			// in case of delete we first delete the node and recreate it again
			if err := ytypes.DeleteNode(schema, curGoStruct, fullPath); err != nil {
				return errors.Wrap(err, fmt.Sprintf("path %v cannot delete path", fullPath))
			}
			emptyNode, _, err = ytypes.GetOrCreateNode(schema, curGoStruct, fullPath)
			if err != nil {
				return errors.Wrap(err, fmt.Sprintf("path %v is not found in the config structure", fullPath))
			}
		} else {
			emptyNode, _, err = ytypes.GetOrCreateNode(schema, curGoStruct, fullPath)
			if err != nil {
				return errors.Wrap(err, fmt.Sprintf("path %v is not found in the config structure", fullPath))
			}
		}

		/*
			if replace ||
				yparser.GnmiPath2XPath(fullPath, true) == "/interface[name=ethernet-1/49]/subinterface[index=1]/ipv4/address[ip-prefix=100.64.0.0/31]" {
				nodeStruct, _ := emptyNode.(ygot.ValidatedGoStruct)
				vvvv, err := ygot.ConstructIETFJSON(nodeStruct, &ygot.RFC7951JSONConfig{})
				if err != nil {
					return nil, errors.Wrap(err, "error in constructing IETF JSON tree from config struct:")
				}
				fmt.Printf("ValidateUpdate vvvv: %v\n", vvvv)
				fmt.Printf("ValidateUpdate nodeStruct: %v\n", nodeStruct)
			}
		*/
		var nodeVal interface{}
		nodeStruct, ok := emptyNode.(ygot.ValidatedGoStruct)
		if ok {
			v := val.GetJsonVal()
			if jsonietf {
				v = val.GetJsonIetfVal()
			}
			/*
				if replace {
					fmt.Printf("ValidateUpdate ok nodeStruct: %v\n", nodeStruct)
				}
			*/

			if err := m.JsonUnmarshaler(v, nodeStruct); err != nil {
				fmt.Printf("ValidateUpdate origin: %s: unmarshal error: nodeStruct: %v\n", origin, nodeStruct)
				fmt.Printf("ValidateUpdate origin: %s: unmarshal error: path: %s, val: %v\n", origin, fullPath, v)
				vvvv, err := ygot.ConstructIETFJSON(nodeStruct, &ygot.RFC7951JSONConfig{})
				if err != nil {
					return errors.Wrap(err, "error in constructing IETF JSON tree from config struct:")
				}
				fmt.Printf("ValidateUpdate origin: %s vvvv: %v\n", origin, vvvv)
				return errors.Wrap(err, "unmarshaling json data to config struct fails")
			}
			if err := nodeStruct.Validate(); err != nil {
				return errors.Wrap(err, "config data validation fails")
			}
			var err error
			if nodeVal, err = ygot.ConstructIETFJSON(nodeStruct, &ygot.RFC7951JSONConfig{}); err != nil {
				return errors.Wrap(err, "error in constructing IETF JSON tree from config struct:")
			}
			//if replace {
			//	fmt.Printf("ValidateUpdate ok nodeVal 1: %v\n", nodeVal)

			//	if err := json.Unmarshal(v, &nodeVal); err != nil {
			//		return nil, errors.Wrap(err, "unmarshaling json data failed")
			//	}
			//	fmt.Printf("ValidateUpdate ok nodeVal 2: %v\n", nodeVal)

			//}
		} else {
			/*
				if replace {
					fmt.Printf("ValidateUpdate nok nodeStruct: %v\n", nodeStruct)
				}
			*/
			var err error
			/*
				if nodeVal, err = value.ToScalar(val); err != nil {
					return nil, errors.Wrap(err, "cannot convert leaf node to scalar type")
				}
			*/
			if nodeVal, err = yparser.GetValue(val); err != nil {
				return errors.Wrap(err, "cannot convert leaf node to scalar type")
			}

			if !replace && origin == Origin_Subscription {
				fmt.Printf("ValidateUpdate origin: %s scalar nodeVal: %v\n", origin, nodeVal)
			}

		}
		// replace or update
		op := gnmi.UpdateResult_UPDATE
		if replace {
			op = gnmi.UpdateResult_REPLACE
		}
		/*
			if op == gnmi.UpdateResult_REPLACE {
				fmt.Printf("ValidateUpdate nodeVal: %v\n", nodeVal)
			}
		*/

		// Update json tree of the device config.
		var curNode interface{} = jsonTree
		schema = m.SchemaTreeRoot
		for i, elem := range fullPath.GetElem() {
			switch node := curNode.(type) {
			case map[string]interface{}:
				// Set node value.
				if i == len(fullPath.GetElem())-1 {
					/*
						if op == gnmi.UpdateResult_REPLACE {
							fmt.Printf("ValidateUpdate: getChildNode last elem, path: %s, index: %d, elem: %v, node: %v, nodeVal: %v\n", yparser.GnmiPath2XPath(fullPath, true), i, elem, node, nodeVal)
						}
					*/

					if len(elem.GetKey()) == 0 {
						// err is grpcstatus error
						//fmt.Printf("ValidateUpdate origin: %s, setPathWithoutAttribute: schemaName: %s, schemaKind: %s\n", origin, schema.Name, schema.Kind.String())

						if err := setPathWithoutAttribute(op, node, elem, nodeVal); err != nil {
							fmt.Printf("ValidateUpdate origin: %s: setPathWithoutAttribute error: %v\n", origin, err)
							return err
						}
						// set defaults
						var newNode map[string]interface{}
						newSchema, ok := schema.Dir[elem.Name]
						if !ok {
							return fmt.Errorf("wrong schema, elem: %s", elem.Name)
						}
						if newSchema.Kind.String() == "Leaf" {
							newNode = node
						} else {
							newNode, ok = node[elem.Name].(map[string]interface{})
							if !ok {
								return fmt.Errorf("wrong node, elem: %s", elem.Name)
							}
						}
						if err := setDefaults(newNode, elem, newSchema, nodeVal); err != nil {
							fmt.Printf("ValidateUpdate origin: %s: setPathWithoutAttribute setDefaults error: %v\n", origin, err)
							return err
						}

						break
					}
					// err is grpcstatus error
					//	fmt.Printf("ValidateUpdate origin: %s: setPathWithAttribute: schemaName: %s, schemaKind: %s\n", origin, schema.Name, schema.Kind.String())
					if err := setPathWithAttribute(op, node, elem, nodeVal, schema); err != nil {
						fmt.Printf("ValidateUpdate origin: %s: setPathWithAttribute: error: %v\n", origin, err)
						return err
					}
					break
				}
				//fmt.Printf("ValidateUpdate: getChildNode before: %s, index: %d, elem: %v, node: %v\n", yparser.GnmiPath2XPath(fullPath, true), i, elem, node)
				if curNode, schema = getChildNode(node, schema, elem, true); curNode == nil {
					return errors.Wrap(err, fmt.Sprintf("path elem not found: %v", elem))
				}
				//fmt.Printf("ValidateUpdate origin: %s: getChildNode after : %s, index: %d, elem: %v, node: %v\n", origin, yparser.GnmiPath2XPath(fullPath, true), i, elem, curNode)
				//fmt.Printf("ValidateUpdate origin: %s: getChildNode after : %s, schemaDir: %v\n", origin, yparser.GnmiPath2XPath(fullPath, true), schema.Dir)
			case []interface{}:
				return errors.Wrap(err, fmt.Sprintf("incompatible path elem: %v", elem))
			default:
				return errors.Wrap(err, fmt.Sprintf("wrong node type: %T", curNode))
			}
		}
	}
	fmt.Printf("ValidateUpdate origin: %s updates finished\n", origin)

	jsonDump, err := json.Marshal(jsonTree)
	if err != nil {
		fmt.Printf("ValidateUpdate origin: %s marshal error :%v\n", origin, err)
		return fmt.Errorf("error in marshaling IETF JSON tree to bytes: %v", err)
	}

	newGoStruct, err := m.NewConfigStruct(jsonDump, true)
	if err != nil {
		fmt.Printf("ValidateUpdate origin: %s creating gostruct error :%v\n", origin, err)
		return fmt.Errorf("error in creating config struct from IETF JSON data: %v", err)
	}

	print := false
	for _, u := range updates {
		fullPath := cleanPath(u.GetPath())
		if strings.Contains(yparser.GnmiPath2XPath(fullPath, true), "/interface[name=ethernet-1/49]") {
			print = true
		}
	}

	if newGoStruct == nil {
		fmt.Printf("ValidateUpdate origin: %s subscription handler handleUpdates suicide empty goStruct\n", origin)
		return errors.New("subscription handler handleUpdates suicide empty goStruct")
	}
	if origin == Origin_Subscription || origin == Origin_GnmiServer {
		if print {
			j, err := ygot.EmitJSON(newGoStruct, &ygot.EmitJSONConfig{
				Format: ygot.RFC7951,
			})
			if err != nil {
				fmt.Println(err)
			}
			fmt.Printf("ValidateUpdate origin: %s jsonTree done: target %s \n, %s \n", origin, ce.GetId(), j)
		}
		ce.SetRunningConfig(newGoStruct)
		fmt.Printf("ValidateUpdate origin: %s stored\n", origin)
	}

	return nil
}

func ValidateDelete(ce cache.CacheEntry, paths []*gnmi.Path, origin Origin) error {
	curGoStruct := ce.GetRunningConfig()

	if curGoStruct == nil {
		return fmt.Errorf("ValidateDelete origin: %s using empty go struct", origin)
	}

	jsonTree, err := ygot.ConstructIETFJSON(curGoStruct, &ygot.RFC7951JSONConfig{})
	if err != nil {
		return errors.Wrap(err, "error in constructing IETF JSON tree from config struct")
	}
	m := ce.GetModel()

	var goStruct ygot.ValidatedGoStruct
	pathDeleted := false
	for _, path := range paths {
		var curNode interface{} = jsonTree
		fullPath := cleanPath(path)
		fmt.Printf("ValidateDelete origin: %s path: %s\n", origin, yparser.GnmiPath2XPath(fullPath, true))

		// we need to go back to the schemaroot for the next delete path
		schema := m.SchemaTreeRoot

		for i, elem := range fullPath.GetElem() { // Delete sub-tree or leaf node.
			node, ok := curNode.(map[string]interface{})
			if !ok {
				// we can break since the element does not exist in the schema
				break
			}

			// Delete node
			if i == len(fullPath.GetElem())-1 {
				//c.log.Debug("getChildNode last elem", "path", yparser.GnmiPath2XPath(fullPath, true), "elem", elem, "node", node)
				//fmt.Printf("ValidateDelete last elem, path: %s, index: %d, elem: %v, node: %v\n", yparser.GnmiPath2XPath(fullPath, true), i, elem, node)
				if len(elem.GetKey()) == 0 {
					// check schema for defaults and fallback to default if required
					if schema, ok = schema.Dir[elem.GetName()]; ok {
						if len(schema.Default) > 0 {
							if err := setPathWithoutAttribute(gnmi.UpdateResult_UPDATE, node, elem, schema.Default[0]); err != nil {
								return err
							}
							// should be update, but we abuse pathDeleted
							pathDeleted = true
							break
						}
					}
					delete(node, elem.GetName())
					pathDeleted = true
					break
				}
				pathDeleted = deleteKeyedListEntry(node, elem)
				break
			}
			//fmt.Printf("ValidateDelete: getChildNode before: %s, index: %d, elem: %v, node: %v\n", yparser.GnmiPath2XPath(fullPath, true), i, elem, node)
			if curNode, schema = getChildNode(node, schema, elem, false); curNode == nil {
				break
			}
			//fmt.Printf("ValidateDelete: getChildNode after: %s, index: %d, elem: %v, node: %v\n", yparser.GnmiPath2XPath(fullPath, true), i, elem, curNode)
			/*
				if pathDeleted {
					jsonDump, err := json.Marshal(jsonTree)
					if err != nil {
						return fmt.Errorf("error in marshaling IETF JSON tree to bytes: %v", err)
					}
					goStruct, err = m.NewConfigStruct(jsonDump, true)
					if err != nil {
						return fmt.Errorf("error in creating config struct from IETF JSON data: %v", err)
					}
				}
			*/
		}
	}
	// Validate the new config
	if pathDeleted {
		jsonDump, err := json.Marshal(jsonTree)
		if err != nil {
			return fmt.Errorf("error in marshaling IETF JSON tree to bytes: %v", err)
		}
		goStruct, err = m.NewConfigStruct(jsonDump, true)
		if err != nil {
			return fmt.Errorf("error in creating config struct from IETF JSON data: %v", err)
		}
		if goStruct == nil {
			//c.log.Debug("handleDeletes UpdateValidatedGoStruct", "deviceName", crDeviceName, "goStruct", goStruct, "delPaths", delPaths)
			return errors.New("subscription handler handleDeletes suicide empty goStruct")
		}
		// update the config with the new struct
		//c.log.Debug("UpdateValidatedGoStruct", "goStruct", goStruct, "delPaths", delPaths)
		ce.SetRunningConfig(goStruct)
	}

	return nil
}

func cleanPath(path *gnmi.Path) *gnmi.Path {
	// clean the path for now to remove the module information from the pathElem
	p := yparser.DeepCopyGnmiPath(path)
	for _, pe := range p.GetElem() {
		pe.Name = strings.Split(pe.Name, ":")[len(strings.Split(pe.Name, ":"))-1]
		keys := make(map[string]string)
		for k, v := range pe.GetKey() {
			if strings.Contains(v, "::") {
				keys[strings.Split(k, ":")[len(strings.Split(k, ":"))-1]] = v
			} else {
				keys[strings.Split(k, ":")[len(strings.Split(k, ":"))-1]] = strings.Split(v, ":")[len(strings.Split(v, ":"))-1]
			}
		}
		pe.Key = keys
	}
	return p
}

// setPathWithoutAttribute replaces or updates a child node of curNode in the
// IETF config tree, where the child node is indexed by pathElem without
// attribute. The function returns grpc status error if unsuccessful.
func setPathWithoutAttribute(op gnmi.UpdateResult_Operation, curNode map[string]interface{}, pathElem *gnmi.PathElem, nodeVal interface{}) error {
	//fmt.Printf("ValidateUpdate: setPathWithoutAttribute, operation: %v, elem: %v, node: %v, nodeVal: %v\n", op, pathElem, curNode, nodeVal)
	target, hasElem := curNode[pathElem.Name]
	nodeValAsTree, nodeValIsTree := nodeVal.(map[string]interface{})
	if op == gnmi.UpdateResult_REPLACE || !hasElem || !nodeValIsTree {
		curNode[pathElem.Name] = nodeVal
		//fmt.Printf("ValidateUpdate: curNode: %v, nodeVal: %v\n", curNode, nodeVal)
		return nil
	}
	targetAsTree, ok := target.(map[string]interface{})
	if !ok {
		return status.Errorf(codes.Internal, "error in setting path: expect map[string]interface{} to update, got %T", target)
	}
	for k, v := range nodeValAsTree {
		targetAsTree[k] = v
	}

	return nil
}

// setPathWithAttribute replaces or updates a child node of curNode in the IETF
// JSON config tree, where the child node is indexed by pathElem with attribute.
// The function returns grpc status error if unsuccessful.
func setPathWithAttribute(op gnmi.UpdateResult_Operation, curNode map[string]interface{}, elem *gnmi.PathElem, nodeVal interface{}, schema *yang.Entry) error {
	//fmt.Printf("ValidateUpdate: setPathWithAttribute, operation: %v, elem: %v, node: %v, nodeVal: %v\n", op, pathElem, curNode, nodeVal)
	nodeValAsTree, ok := nodeVal.(map[string]interface{})
	if !ok {
		return status.Errorf(codes.InvalidArgument, "expect nodeVal is a json node of map[string]interface{}, received %T", nodeVal)
	}
	m := getKeyedListEntry(curNode, elem, true)
	if m == nil {
		return status.Errorf(codes.NotFound, "path elem not found: %v", elem)
	}
	if op == gnmi.UpdateResult_REPLACE {
		for k := range m {
			//fmt.Printf("ValidateUpdate: setPathWithAttribute 1: k: %v, m: %v\n", k, m)
			delete(m, k)
		}
	}
	// Debug to be removed below
	//if op == gnmi.UpdateResult_REPLACE {
	//	fmt.Printf("ValidateUpdate: setPathWithAttribute 2: m: %v\n", m)
	//}
	// Debug to be removed above
	for attrKey, attrVal := range elem.GetKey() {
		m[attrKey] = attrVal
		if asNum, err := strconv.ParseFloat(attrVal, 64); err == nil {
			m[attrKey] = asNum
		}
		for k, v := range nodeValAsTree {
			if k == attrKey && fmt.Sprintf("%v", v) != attrVal {
				return status.Errorf(codes.InvalidArgument, "invalid config data: %v is a path attribute", k)
			}
		}
		// Debug to be removed below
		//if op == gnmi.UpdateResult_REPLACE {
		//	fmt.Printf("ValidateUpdate: setPathWithAttribute 3: m: %v\n", m)
		//}
		// Debug to be removed above
	}
	for k, v := range nodeValAsTree {
		m[k] = v
		// Debug to be removed below
		//if op == gnmi.UpdateResult_REPLACE {
		//	fmt.Printf("ValidateUpdate: setPathWithAttribute 4: k: %v, v: %v\n", k, v)
		//}
		// Debug to be removed above
	}
	// Debug to be removed below
	//if op == gnmi.UpdateResult_REPLACE {
	//	fmt.Printf("ValidateUpdate: setPathWithAttribute 5: m: %v\n", m)
	//	fmt.Printf("ValidateUpdate: setPathWithAttribute 5: curNode: %v\n", curNode)
	//}
	// Debug to be removed above

	// set defaults
	/*
		newSchema, ok := schema.Dir[pathElem.GetName()]
		if ok {
			if err := setDefaults(m, newSchema); err != nil {
				fmt.Printf("set default error: %v\n", err)
				return err
			}
		}
	*/
	// set defaults
	newSchema, ok := schema.Dir[elem.Name]
	if !ok {
		return fmt.Errorf("wrong schema, elem: %s", elem.Name)
	}
	if err := setDefaults(m, elem, newSchema, nodeValAsTree); err != nil {
		fmt.Printf("set default error: %v\n", err)
		return err
	}

	return nil
}

// deleteKeyedListEntry deletes the keyed list entry from node that matches the
// path elem. If the entry is the only one in keyed list, deletes the entire
// list. If the entry is found and deleted, the function returns true. If it is
// not found, the function returns false.
func deleteKeyedListEntry(node map[string]interface{}, elem *gnmi.PathElem) bool {
	curNode, ok := node[elem.Name]
	if !ok {
		return false
	}

	keyedList, ok := curNode.([]interface{})
	if !ok {
		return false
	}
	for i, n := range keyedList {
		m, ok := n.(map[string]interface{})
		if !ok {
			fmt.Printf("expect map[string]interface{} for a keyed list entry, got %T", n)
			return false
		}
		keyMatching := true
		for k, v := range elem.Key {
			attrVal, ok := m[k]
			if !ok {
				return false
			}
			if v != fmt.Sprintf("%v", attrVal) {
				keyMatching = false
				break
			}
		}
		if keyMatching {
			listLen := len(keyedList)
			if listLen == 1 {
				delete(node, elem.Name)
				return true
			}
			keyedList[i] = keyedList[listLen-1]
			node[elem.Name] = keyedList[0 : listLen-1]
			return true
		}
	}
	return false
}

// getChildNode gets a node's child with corresponding schema specified by path
// element. If not found and createIfNotExist is set as true, an empty node is
// created and returned.
func getChildNode(node map[string]interface{}, schema *yang.Entry, elem *gnmi.PathElem, createIfNotExist bool) (interface{}, *yang.Entry) {
	var nextSchema *yang.Entry
	var ok bool

	//fmt.Printf("elem name: %s, key: %v\n", elem.GetName(), elem.GetKey())
	//c.log.Debug("getChildNode", "elem name", elem.GetName(), "elem key", elem.GetKey())

	if nextSchema, ok = schema.Dir[elem.GetName()]; !ok {
		// returning nil will be picked up as an error
		return nil, nil
	}

	var nextNode interface{}
	if len(elem.GetKey()) == 0 {
		//c.log.Debug("getChildNode container", "elem name", elem.GetName(), "elem key", elem.GetKey())
		if nextNode, ok = node[elem.GetName()]; !ok {
			//c.log.Debug("getChildNode new container entry", "elem name", elem.GetName(), "elem key", elem.GetKey())
			if createIfNotExist {
				node[elem.Name] = make(map[string]interface{})
				nextNode = node[elem.GetName()]
			}
		}
		return nextNode, nextSchema
	}

	nextNode = getKeyedListEntry(node, elem, createIfNotExist)
	return nextNode, nextSchema
}

// getKeyedListEntry finds the keyed list entry in node by the name and key of
// path elem. If entry is not found and createIfNotExist is true, an empty entry
// will be created (the list will be created if necessary).
func getKeyedListEntry(node map[string]interface{}, elem *gnmi.PathElem, createIfNotExist bool) map[string]interface{} {
	//c.log.Debug("getKeyedListEntry", "elem name", elem.GetName(), "elem key", elem.GetKey())
	curNode, ok := node[elem.GetName()]
	if !ok {
		if !createIfNotExist {
			return nil
		}

		// Create a keyed list as node child and initialize an entry.
		m := make(map[string]interface{})
		for k, v := range elem.GetKey() {
			m[k] = v
			if vAsNum, err := strconv.ParseFloat(v, 64); err == nil {
				m[k] = vAsNum
			}
		}
		node[elem.GetName()] = []interface{}{m}
		return m
	}

	// Search entry in keyed list.
	keyedList, ok := curNode.([]interface{})
	if !ok {
		switch m := curNode.(type) {
		case map[string]interface{}:
			return m
		default:
			return nil

		}

	}
	for _, n := range keyedList {
		m, ok := n.(map[string]interface{})
		if !ok {
			fmt.Printf("wrong keyed list entry type: %T", n)
			return nil
		}
		keyMatching := true
		// must be exactly match
		for k, v := range elem.GetKey() {
			attrVal, ok := m[k]
			if !ok {
				return nil
			}
			if v != fmt.Sprintf("%v", attrVal) {
				keyMatching = false
				break
			}
		}
		if keyMatching {
			return m
		}
	}
	if !createIfNotExist {
		return nil
	}

	// Create an entry in keyed list.
	m := make(map[string]interface{})
	for k, v := range elem.GetKey() {
		m[k] = v
		if vAsNum, err := strconv.ParseFloat(v, 64); err == nil {
			m[k] = vAsNum
		}
	}
	node[elem.GetName()] = append(keyedList, m)
	return m
}

func setDefaults(node map[string]interface{}, pathElem *gnmi.PathElem, schema *yang.Entry, nodeVal interface{}) error {
	// check schema for defaults and fallback to default if required
	if schema.Kind.String() == "Leaf" {
		// this is a leaf
		if len(schema.Default) > 0 && !schema.ReadOnly() {
			//fmt.Printf("ValidateUpdate: set default value: elem: %s, schema default: %v, node: %v, readOnly: %t\n", pathElem.GetName(), schema.Default, node, schema.ReadOnly())
			setDefaultValue(node, pathElem.GetName(), schema)

		}
		return nil
	}
	// this is a directory
	for elem, schema := range schema.Dir {
		//fmt.Printf("ValidateUpdate: set default: elem: %s,  schema default: %v, pathElem: %s, readOnly: %t\n", elem, schema.Default, pathElem.Name, schema.ReadOnly())
		// only update the default if the update path is not equal to the pathElem
		if len(schema.Default) > 0 && !schema.ReadOnly() {
			// check if the default value is not part
			nodeVal, ok := nodeVal.(map[string]interface{})
			if ok {
				// this is a json value
				// this is a default and the default was not part of the json value we add the default in the jsonTree
				if _, ok := nodeVal[elem]; !ok {
					//fmt.Printf("ValidateUpdate: set default value: elem: %s, schema default: %v, node: %v\n", elem, schema.Default, node)
					if err := setDefaultValue(node, elem, schema); err != nil {
						return err
					}
				}
			} else {
				// this is a scalar value
				if pathElem.Name != elem {
					//fmt.Printf("ValidateUpdate: set default value: elem: %s, schema default: %v, node: %v\n", elem, schema.Default, node)
					if err := setDefaultValue(node, elem, schema); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func setDefaultValue(node map[string]interface{}, elem string, schema *yang.Entry) error {
	//fmt.Printf("ValidateUpdate: default elem: %s, val: %v, type: %s\n", elem, schema.Default, schema.Type.Kind.String())
	var nodeVal interface{}
	nodeVal = schema.Default[0]
	switch schema.Type.Kind.String() {
	case "boolean":
		v, err := strconv.ParseBool(schema.Default[0])
		if err != nil {
			return err
		}
		if nodeVal, err = value.ToScalar(&gnmi.TypedValue{Value: &gnmi.TypedValue_BoolVal{BoolVal: v}}); err != nil {
			return errors.Wrap(err, "cannot convert leaf node to scalar type")
		}
		//fmt.Printf("ValidateUpdate: elem: %s nodeVal: %#v\n", elem, nodeVal)
	case "uint8", "uint16", "uint32", "uint64", "int8", "int16", "int32", "int64":
		d, err := strconv.Atoi(schema.Default[0])
		if err != nil {
			return err
		}
		switch schema.Type.Kind.String() {
		case "uint8":
			nodeVal = uint8(d)
		case "uint16":
			nodeVal = uint16(d)
		case "uint32":
			nodeVal = uint32(d)
		case "uint64":
			nodeVal = uint64(d)
		case "int8":
			nodeVal = int8(d)
		case "int16":
			nodeVal = int16(d)
		case "int32":
			nodeVal = int32(d)
		case "int64":
			nodeVal = int64(d)
		}
	}
	if err := setPathWithoutAttribute(gnmi.UpdateResult_UPDATE, node, &gnmi.PathElem{Name: elem}, nodeVal); err != nil {
		return err
	}
	return nil
}
