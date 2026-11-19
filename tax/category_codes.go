package tax

import (
	"encoding/json"
	"slices"

	"github.com/invopop/gobl/cbc"
	"github.com/invopop/jsonschema"
)

// CategoryCodes is a list of tax category codes, such as the categories
// already included in the prices of a document. In JSON, a list with a
// single code is written as a plain string, so documents that use a single
// category keep their original form, and a plain string is read as a list
// of one.
type CategoryCodes []cbc.Code

// Has returns true if the list contains the category code.
func (cc CategoryCodes) Has(code cbc.Code) bool {
	return slices.Contains(cc, code)
}

// IsEmpty returns true if the list has no codes.
func (cc CategoryCodes) IsEmpty() bool {
	return len(cc) == 0
}

// MarshalJSON writes a single code as a plain string, and several codes
// as an array.
func (cc CategoryCodes) MarshalJSON() ([]byte, error) {
	if len(cc) == 1 {
		return json.Marshal(cc[0])
	}
	return json.Marshal([]cbc.Code(cc))
}

// UnmarshalJSON reads either a plain string or an array of codes.
func (cc *CategoryCodes) UnmarshalJSON(data []byte) error {
	var code cbc.Code
	if err := json.Unmarshal(data, &code); err == nil {
		*cc = nil
		if code != cbc.CodeEmpty {
			*cc = CategoryCodes{code}
		}
		return nil
	}
	var codes []cbc.Code
	if err := json.Unmarshal(data, &codes); err != nil {
		return err
	}
	*cc = codes
	return nil
}

// JSONSchema accepts either a single category code or a list of them.
func (CategoryCodes) JSONSchema() *jsonschema.Schema {
	code := &jsonschema.Schema{
		Type:    "string",
		Pattern: cbc.CodePattern,
	}
	return &jsonschema.Schema{
		OneOf: []*jsonschema.Schema{
			code,
			{
				Type:  "array",
				Items: code,
			},
		},
	}
}
