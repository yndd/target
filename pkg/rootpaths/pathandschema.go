package rootpaths

import (
	"fmt"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/goyang/pkg/yang"
)

type PathAndSchema struct {
	path   *gnmi.Path
	schema *yang.Entry
}

// retrieve the schema
func (pas *PathAndSchema) GetSchema() *yang.Entry {
	return pas.schema
}

// retrieve the path
func (pas *PathAndSchema) GetPath() *gnmi.Path {
	return pas.path
}

func (pas *PathAndSchema) IsKey() bool {
	return pas.schema.Name == pas.schema.Parent.Key
}

// runs from schema root through to the schema element that the gnmi.Update referes to
// and returns the corresponding *yang.Entry.
func GetPathAndSchemaEntry(rootschema *yang.Entry, u *gnmi.Path) *PathAndSchema {
	var schema = rootschema
	for _, elem := range u.Elem {
		schema = schema.Dir[elem.Name]
	}
	return &PathAndSchema{path: u, schema: schema}
}

type PathAndSchemaCount struct {
	*PathAndSchema
	count uint
}

func (pasc *PathAndSchemaCount) String() string {
	return fmt.Sprintf("Count: %d, Path: %s", pasc.count, pasc.path.String())
}
