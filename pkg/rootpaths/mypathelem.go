package rootpaths

import "github.com/openconfig/gnmi/proto/gnmi"

// MyPathElem - A wrapper for the gnmi.PathElem.
// Used to be able to implement a custom hashCode() method, to be able to allow for map lookups
// not just on identity (pointer equality) but in this case, name and key equality
type MyPathElem struct {
	*gnmi.PathElem
}

// hashCode defines the hashCode calculation for the MyPathElem, which is a wrapper around the gnmi.PathElem.
// The hashCode is the name concatinated with the keys as key values.
func (pe *MyPathElem) hashCode() string {
	// returning the generated hashCode
	return pe.Name + getSortedKeyMapAsString(pe.Key)
}
