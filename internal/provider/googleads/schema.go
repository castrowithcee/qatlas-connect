package googleads

import (
	"encoding/json"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

// Builders of the JSON schemas of this package. Output values that Google enumerates stay plain strings, so a
// value a later minor version adds does not fail the output schema.
type schema = map[string]any

func object(properties schema, required ...string) schema {
	out := schema{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func arrayOf(items schema) schema { return schema{"type": "array", "items": items} }

var (
	stringSchema  = schema{"type": "string"}
	booleanSchema = schema{"type": "boolean"}
	numberSchema  = schema{"type": "number"}
	integerSchema = schema{"type": "integer"}
)

// encoded is a schema as a descriptor holds it. The schemas are static, so a failure is a programming error.
func encoded(value schema) json.RawMessage {
	out, err := json.Marshal(value)
	if err != nil {
		panic("googleads: a schema cannot be encoded: " + err.Error())
	}
	return out
}

// readRisk is the risk of every read tool of this provider: it reads, is safe to repeat, and reaches the
// account of a third party.
var readRisk = capability.Risk{
	Effect:       capability.EffectRead,
	Idempotency:  capability.IdempotencySafe,
	Confirmation: capability.ConfirmationNone,
	OpenWorld:    true,
	DataSensitivity: "settings and hierarchy of Google Ads accounts: names, currency, time zone, status, " +
		"tracking templates, conversion tracking and verification partners; no data of other people",
}

// customerIDArgument is the schema of a customer_id argument: ten digits without hyphens.
var customerIDArgument = schema{"type": "string", "pattern": "^[0-9]{10}$"}
