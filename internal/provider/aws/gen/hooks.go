package gen

// hookField is the result member a result hook adds. The hook itself is hand-written in the provider package;
// the generator only declares the member, so the output schema accepts what the hook returns.
type hookField struct {
	name, description string
	schema            func() *object
}

// resultHookFields declares the member of every hook in ResultHooks.
var resultHookFields = map[string]hookField{
	"dns-records": {
		name: fieldDNSRecords,
		description: "DNS records to create for the identity, derived by this tool and not copied from the answer: " +
			"DKIM CNAME records pointing into the signing_hosted_zone of the answer (none without a valid one), and the MX and SPF TXT records of a configured MAIL FROM domain; a value of the " +
			"answer that has not the expected form is left out; empty when none apply",
		schema: func() *object {
			item := newObject("type", "object", "properties", newObject(
				"purpose", newObject("type", "string", "enum", []any{"dkim", "mail-from-mx", "mail-from-spf"}),
				"type", newObject("type", "string", "enum", []any{"CNAME", "MX", "TXT"}),
				"name", newObject("type", "string", "maxLength", 400),
				"value", newObject("type", "string", "maxLength", 400),
			), "required", []string{"purpose", "type", "name", "value"}, "additionalProperties", false)
			return newObject("type", "array", "maxItems", 64, "items", item)
		},
	}, "bucket-policy-status": {
		name: fieldPublicStatus,
		description: "Whether S3 evaluates the bucket policy just written as public, read with a call of its own after the write: " +
			"is_public true or false, or unavailable with a reason when the status could not be read. A hint to review, not a check " +
			"and not a guarantee",
		schema: func() *object {
			return newObject("type", "object", "properties", newObject(
				"is_public", newObject("type", "boolean"),
				"unavailable", newObject("type", "string", "maxLength", 300),
			), "additionalProperties", false)
		},
	},
}
