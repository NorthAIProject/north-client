package ai

// Schema is a provider-neutral description of a JSON shape.
//
// It is deliberately a small subset of JSON Schema: the parts every provider
// supports and North actually needs. A richer schema would be portable in
// theory and rejected in practice, since providers disagree about the corners.
//
// Constraining output this way is North's main defence against a coach that
// invents structure. A workout plan comes back as a plan or not at all; it
// cannot come back as prose that happens to mention sets and reps.
type Schema struct {
	Type        SchemaType
	Description string

	// Enum restricts a string to a fixed set. Preferred over a description
	// asking politely for one of several values.
	Enum []string

	// Items is the element schema when Type is TypeArray.
	Items *Schema

	// Properties are the fields when Type is TypeObject.
	Properties map[string]*Schema

	// Required lists properties that must be present. Providers vary in how
	// strictly they honour this, so validate after decoding as well.
	Required []string

	// PropertyOrdering fixes the order fields are generated in. Gemini uses
	// this, and order matters more than it looks: a model that writes its
	// reasoning before its conclusion produces better conclusions.
	PropertyOrdering []string
}

type SchemaType string

const (
	TypeObject  SchemaType = "object"
	TypeArray   SchemaType = "array"
	TypeString  SchemaType = "string"
	TypeNumber  SchemaType = "number"
	TypeInteger SchemaType = "integer"
	TypeBoolean SchemaType = "boolean"
)

// Object builds an object schema whose properties are all required, in the
// given order. Optional fields are rare in North's schemas: a field the model
// may omit is a field the caller has to nil-check forever.
func Object(description string, properties map[string]*Schema, order ...string) *Schema {
	if len(order) == 0 {
		order = keysOf(properties)
	}
	return &Schema{
		Type:             TypeObject,
		Description:      description,
		Properties:       properties,
		Required:         order,
		PropertyOrdering: order,
	}
}

func Array(description string, items *Schema) *Schema {
	return &Schema{Type: TypeArray, Description: description, Items: items}
}

func String(description string) *Schema {
	return &Schema{Type: TypeString, Description: description}
}

func Enum(description string, values ...string) *Schema {
	return &Schema{Type: TypeString, Description: description, Enum: values}
}

func Integer(description string) *Schema {
	return &Schema{Type: TypeInteger, Description: description}
}

func Number(description string) *Schema {
	return &Schema{Type: TypeNumber, Description: description}
}

func Boolean(description string) *Schema {
	return &Schema{Type: TypeBoolean, Description: description}
}

// keysOf returns property names in a stable order.
//
// Map iteration in Go is deliberately randomised, so without sorting, the same
// schema would produce a different property ordering on every process start and
// the model's output would drift for no visible reason.
func keysOf(properties map[string]*Schema) []string {
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sortStrings(names)
	return names
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
