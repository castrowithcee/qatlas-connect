package gen

const (
	descR53CidrName = "Name of the collection, 1 to 64 letters, digits, hyphens, and underscores"
	// r53CidrConfirm is the text the confirmation of a CIDR collection write has to carry.
	r53CidrConfirm = " The confirmation names the tool, not the arguments: show the collection (name or ID) and, for a change, every " +
		"change (action, location_name, and each cidr_block) before confirming."
)

// route53CIDRTools are the CIDR collections that routing records of the account refer to.
var route53CIDRTools = []Operation{
	r53Write(Operation{
		Operation: "CreateCidrCollection", Object: "cidr-collection", Action: "create", Effect: "create", Idempotency: "non-idempotent",
		Title: "Create a Route 53 CIDR collection",
		Description: "Create an empty CIDR collection, a named set of locations that each hold CIDR blocks; IP-based routing records refer to it. " +
			"Fill it with aws.route53.cidr-collection.change. A name that exists answers CidrCollectionAlreadyExistsException, " +
			"the same token with another name ConcurrentModification, and the account's quota LimitsExceeded. Needs route53:CreateCidrCollection. " +
			"The call token is generated and returned as client_token." + r53CidrConfirm,
		Arguments: map[string]Argument{"Name": {Description: descR53CidrName}},
		Fields: map[string]Field{
			"Collection": {Description: "The new collection: arn, id (a UUID, used by the other CIDR tools), name, and version"},
			"Location":   {Description: "Path of the new collection, as Route 53 reports it"},
		},
		TokenMember: "CallerReference",
		Examples:    []Example{{Description: "Create a collection", Arguments: `{"name":"offices"}`}},
	}, "cidr-collection"),
	r53Write(Operation{
		Operation: "ChangeCidrCollection", Object: "cidr-collection", Action: "change", Effect: "update", Idempotency: "non-idempotent",
		Title: "Change the locations and CIDR blocks of a Route 53 CIDR collection",
		Description: "Add or remove CIDR blocks of a collection in one call. Each change names a location_name (1 to 16 letters, digits, hyphens, " +
			"and underscores; a PUT creates the location), an action (PUT adds the blocks, DELETE_IF_EXISTS removes them, and a location " +
			"without blocks is removed), and cidr_list, IPv4 or IPv6 blocks in CIDR notation such as 192.0.2.0/24 or 2001:db8::/48. " +
			"The tool refuses a block that is not in CIDR notation, and more than 1000 changes or 1000 blocks in all, before any request; " +
			"Route 53 judges the rest (the largest block sizes, the quota). collection_version is the precondition: " +
			"read it with aws.route53.cidr-collection.list, and when the collection changed in between Route 53 answers " +
			"CidrCollectionVersionMismatchException and changes nothing. Every successful call raises the version, so repeating a call with " +
			"the same version fails and the call is not repeated automatically. A collection a routing record uses cannot lose a location " +
			"it needs (CidrCollectionInUseException), an unknown collection answers NoSuchCidrCollectionException, and a block " +
			"that is not valid InvalidInput. Needs route53:ChangeCidrCollection. The result is the ID of the change." + r53CidrConfirm,
		Arguments: map[string]Argument{
			"Id":                {Description: "ID of the collection, a UUID"},
			"CollectionVersion": {Description: "Version of the collection the change is based on; the change is refused when the collection has another version. Left out: no check"},
			"Changes": {Description: "At least one change, each with location_name, action (PUT or DELETE_IF_EXISTS), and cidr_list " +
				"(the blocks, such as 192.0.2.0/24 or 2001:db8::/48)"},
		},
		Fields: map[string]Field{
			"Id": {Description: "ID of the change, as Route 53 reports it"},
		},
		Guards:     []string{"route53-cidr"},
		SampleArgs: `{"id":"01234567-89ab-cdef-0123-456789abcdef","changes":[{"location_name":"a","action":"PUT","cidr_list":["192.0.2.0/24"]}]}`,
		Examples: []Example{
			{Description: "Add blocks to a location", Arguments: `{"id":"01234567-89ab-cdef-0123-456789abcdef","collection_version":3,"changes":[{"location_name":"berlin","action":"PUT","cidr_list":["192.0.2.0/24","2001:db8::/48"]}]}`},
			{Description: "Remove a block", Arguments: `{"id":"01234567-89ab-cdef-0123-456789abcdef","changes":[{"location_name":"berlin","action":"DELETE_IF_EXISTS","cidr_list":["192.0.2.0/24"]}]}`},
		},
	}, "cidr-collection"),
	r53Write(Operation{
		Operation: "DeleteCidrCollection", Object: "cidr-collection", Action: "delete", Effect: "delete", Idempotency: "non-idempotent",
		Title: "Delete a Route 53 CIDR collection",
		Description: "Delete a CIDR collection that holds no locations (remove them with aws.route53.cidr-collection.change first): a collection " +
			"with locations answers CidrCollectionInUseException and stays, an unknown one NoSuchCidrCollectionException. A deleted collection " +
			"cannot be restored. Needs route53:DeleteCidrCollection." + r53CidrConfirm,
		Arguments: map[string]Argument{"Id": {Description: "ID of the collection, a UUID"}},
		Examples:  []Example{{Description: "Delete an empty collection", Arguments: `{"id":"01234567-89ab-cdef-0123-456789abcdef"}`}},
	}, "cidr-collection"),
}
