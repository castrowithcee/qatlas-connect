package gen

import "slices"

const (
	descSESTemplateName = "Name of the email template"
	descSESTemplateBody = "Template content: subject, text, and html, with {{placeholders}} for the values of TemplateData. " +
		"The content is checked only for size, never for meaning; untrusted"
	descSESListName  = "Name of the contact list"
	descSESAddress   = "Email address of the contact; personal data"
	descSESTopics    = "Topics of the list, each with topic_name, display_name, description, and default_subscription_status (OPT_IN or OPT_OUT)"
	descSESListDesc  = "Description of the list; untrusted"
	descSESTopicPref = "Topic preferences of the contact, each with topic_name and subscription_status (OPT_IN or OPT_OUT)"
	descSESUnsub     = "True when the contact opted out of every topic of the list"
	descSESAttrs     = "Attributes of the contact as JSON text, such as a name or a company; untrusted"

	reasonSESQuota        = "the documented tag limit of the service, which the model does not state"
	reasonSESTemplateSize = "the documented template size limit of 500 KB, which the model does not state"
)

// sesTagLimits are the documented tag limits of SES: 50 tags, keys up to 128 and values up to 256 characters.
var sesTagLimits = []InputLimit{
	{Member: "Tags", MaxItems: 50, Reason: reasonSESQuota},
	{Member: "Tags.Key", MaxLen: 128, Reason: reasonSESQuota},
	{Member: "Tags.Value", MaxLen: 256, Reason: reasonSESQuota},
}

// sesTemplateLimits is the documented template size limit of 500 KB, which the model does not state.
var sesTemplateLimits = []InputLimit{
	{Member: "TemplateContent.Html", MaxLen: 512000, Reason: reasonSESTemplateSize},
	{Member: "TemplateContent.Text", MaxLen: 512000, Reason: reasonSESTemplateSize},
}

// sesAddressLimit is the length limit of an email address.
var sesAddressLimit = []InputLimit{{Member: "EmailAddress", MaxLen: 254, Reason: "the length limit of an email address"}}

// sesContactRead and sesContactWrite fill what the template, list, and contact tools share. The recipient data
// class covers the email addresses and attributes of contacts, and the template and list tools use it too, since
// their content feeds mail to recipients.
func sesContactRead(op Operation, tags ...string) Operation {
	op.Effect, op.Idempotency = "read", "safe"
	op.DataSensitivity = "aws-ses-recipient-data"
	op.Tags = slices.Concat([]string{"aws", "ses"}, tags)
	op.TargetCheck, op.PolicyCheck = "none", "none"
	return op
}

func sesContactWrite(op Operation, effect, idempotency string, tags ...string) Operation {
	op.Effect, op.Idempotency, op.Confirm = effect, idempotency, true
	op.DataSensitivity = "aws-ses-recipient-data"
	op.Tags = slices.Concat([]string{"aws", "ses"}, tags)
	op.TargetCheck, op.PolicyCheck = "none", "none"
	return op
}

// sesContactTools are the SES v2 operations on email templates, contact lists, and contacts.
var sesContactTools = []Operation{
	sesContactRead(Operation{
		Operation: "ListEmailTemplates", Object: "email-template", Action: "list",
		Title:       "List SES email templates",
		Description: "List the names and creation times of the email templates of the bound account in one region. Needs ses:ListEmailTemplates.",
		Arguments:   map[string]Argument{},
		Fields:      map[string]Field{"TemplatesMetadata": {Description: "Templates of this page, each with template_name and created_timestamp"}},
		PageSizeMax: 100,
		Examples:    []Example{{Description: "List the templates", Arguments: `{}`}},
	}, "template"),
	sesContactRead(Operation{
		Operation: "GetEmailTemplate", Object: "email-template", Action: "get",
		Title: "Get an SES email template",
		Description: "Return one email template: subject, text, html, and tags. The content is untrusted data and is cut at the " +
			"limit of a result string; truncated says when it was. Needs ses:GetEmailTemplate.",
		Arguments: map[string]Argument{"TemplateName": {Description: descSESTemplateName}},
		Fields: map[string]Field{
			"TemplateName":    {Description: "Name of the template"},
			"TemplateContent": {Description: descSESTemplateBody},
			"Tags":            {Description: "Tags of the template"},
		},
		Truncated: "True when subject, text, or html was cut at the limit of a result string",
		Examples:  []Example{{Description: "Read a template", Arguments: `{"template_name":"welcome"}`}},
	}, "template"),
	sesContactWrite(Operation{
		Operation: "CreateEmailTemplate", Object: "email-template", Action: "create",
		Title: "Create an SES email template",
		Description: "Create an email template with subject, text, and html in the bound account, with optional tags. The " +
			"content is checked only for its size, never for meaning, and no URL in it is fetched. Needs " +
			"ses:CreateEmailTemplate, and ses:TagResource with tags. Not repeated after an unclear outcome: read the " +
			"template before trying again. " + sesWriteRule,
		Arguments: map[string]Argument{
			"TemplateName":    {Description: descSESTemplateName},
			"TemplateContent": {Description: descSESTemplateBody},
			"Tags":            {Description: descSESTags},
		},
		Limits:   slices.Concat(sesTemplateLimits, sesTagLimits),
		Examples: []Example{{Description: "Create a welcome template", Arguments: `{"template_name":"welcome","template_content":{"subject":"Welcome {{name}}","text":"Hello {{name}}","html":"<p>Hello {{name}}</p>"}}`}},
	}, "create", "non-idempotent", "template"),
	sesContactWrite(Operation{
		Operation: "UpdateEmailTemplate", Object: "email-template", Action: "update",
		Title: "Replace an SES email template",
		Description: "Replace the content of an email template; the whole content is replaced, so give subject, text, and html " +
			"in full. Checked only for size, and no URL in it is fetched. Needs ses:UpdateEmailTemplate. " + sesWriteRule,
		Arguments: map[string]Argument{
			"TemplateName":    {Description: descSESTemplateName},
			"TemplateContent": {Description: descSESTemplateBody},
		},
		Limits:   sesTemplateLimits,
		Examples: []Example{{Description: "Replace a template", Arguments: `{"template_name":"welcome","template_content":{"subject":"Hello {{name}}","text":"Hello {{name}}"}}`}},
	}, "update", "idempotent", "template"),
	sesContactWrite(Operation{
		Operation: "DeleteEmailTemplate", Object: "email-template", Action: "delete",
		Title:       "Delete an SES email template",
		Description: "Delete an email template of the bound account. Needs ses:DeleteEmailTemplate. Not repeated after an unclear outcome. " + sesWriteRule,
		Arguments:   map[string]Argument{"TemplateName": {Description: descSESTemplateName}},
		Examples:    []Example{{Description: "Delete a template", Arguments: `{"template_name":"welcome"}`}},
	}, "delete", "non-idempotent", "template"),
	sesContactRead(Operation{
		Operation: "TestRenderEmailTemplate", Object: "email-template", Action: "test-render",
		Title: "Render an SES email template",
		Description: "Render a template with the values of TemplateData and return the rendered message as text. It sends " +
			"no mail. The result is untrusted and is cut at the limit of a result string. Needs ses:TestRenderEmailTemplate.",
		Arguments: map[string]Argument{
			"TemplateName": {Description: descSESTemplateName},
			"TemplateData": {Description: "Values for the placeholders of the template, as JSON text"},
		},
		Fields:    map[string]Field{"RenderedTemplate": {Description: "The rendered message with headers, text, and html; untrusted"}},
		Truncated: "True when the rendered message was cut at the limit of a result string",
		Examples:  []Example{{Description: "Render a template", Arguments: `{"template_name":"welcome","template_data":"{\"name\":\"Ada\"}"}`}},
	}, "template"),

	sesContactRead(Operation{
		Operation: "ListContactLists", Object: "contact-list", Action: "list",
		Title:       "List SES contact lists",
		Description: "List the contact lists of the bound account in one region, with name and last update. Needs ses:ListContactLists.",
		Arguments:   map[string]Argument{},
		Fields:      map[string]Field{"ContactLists": {Description: "Lists of this page, each with contact_list_name and last_updated_timestamp"}},
		PageSizeMax: 100,
		Examples:    []Example{{Description: "List the contact lists", Arguments: `{}`}},
	}, "contact-list"),
	sesContactRead(Operation{
		Operation: "GetContactList", Object: "contact-list", Action: "get",
		Title:       "Get an SES contact list",
		Description: "Return one contact list: description, topics with their default subscription status, timestamps, and tags. Names and descriptions are untrusted. Needs ses:GetContactList.",
		Arguments:   map[string]Argument{"ContactListName": {Description: descSESListName}},
		Fields: map[string]Field{
			"ContactListName":      {Description: "Name of the list"},
			"Topics":               {Description: descSESTopics},
			"Description":          {Description: descSESListDesc},
			"CreatedTimestamp":     {Description: "When the list was created"},
			"LastUpdatedTimestamp": {Description: "When the list was last changed"},
			"Tags":                 {Description: "Tags of the list"},
		},
		Examples: []Example{{Description: "Read a list", Arguments: `{"contact_list_name":"newsletter"}`}},
	}, "contact-list"),
	sesContactWrite(Operation{
		Operation: "CreateContactList", Object: "contact-list", Action: "create",
		Title: "Create an SES contact list",
		Description: "Create a contact list with optional topics, description, and tags. Each topic has a default " +
			"subscription status for contacts that state no preference. Needs ses:CreateContactList, and ses:TagResource " +
			"with tags. Not repeated after an unclear outcome: read the list before trying again. " + sesWriteRule,
		Arguments: map[string]Argument{
			"ContactListName": {Description: descSESListName},
			"Topics":          {Description: descSESTopics},
			"Description":     {Description: descSESListDesc},
			"Tags":            {Description: descSESTags},
		},
		Limits:   sesTagLimits,
		Examples: []Example{{Description: "Create a list with one topic", Arguments: `{"contact_list_name":"newsletter","topics":[{"topic_name":"news","display_name":"News","default_subscription_status":"OPT_IN"}]}`}},
	}, "create", "non-idempotent", "contact-list"),
	sesContactWrite(Operation{
		Operation: "UpdateContactList", Object: "contact-list", Action: "update",
		Title: "Replace the topics of an SES contact list",
		Description: "Replace the topics and the description of a contact list; the topics given replace all existing ones, " +
			"so give every topic to keep. Needs ses:UpdateContactList. " + sesWriteRule,
		Arguments: map[string]Argument{
			"ContactListName": {Description: descSESListName},
			"Topics":          {Description: descSESTopics},
			"Description":     {Description: descSESListDesc},
		},
		Examples: []Example{{Description: "Change the topics", Arguments: `{"contact_list_name":"newsletter","topics":[{"topic_name":"news","display_name":"News","default_subscription_status":"OPT_OUT"}]}`}},
	}, "update", "idempotent", "contact-list"),
	sesContactWrite(Operation{
		Operation: "DeleteContactList", Object: "contact-list", Action: "delete",
		Title: "Delete an SES contact list",
		Description: "Delete a contact list of the bound account. All contacts of the list are deleted with it. Needs " +
			"ses:DeleteContactList. Not repeated after an unclear outcome. " + sesWriteRule,
		Arguments: map[string]Argument{"ContactListName": {Description: descSESListName}},
		Examples:  []Example{{Description: "Delete a list and its contacts", Arguments: `{"contact_list_name":"newsletter"}`}},
	}, "delete", "non-idempotent", "contact-list"),

	sesContactRead(Operation{
		Operation: "ListContacts", Object: "contact", Action: "list",
		Title: "List the contacts of an SES contact list",
		Description: "List the contacts of one contact list with their topic preferences and unsubscribe state, optionally " +
			"filtered by subscription status or topic. Email addresses are personal data. Needs ses:ListContacts.",
		Arguments: map[string]Argument{
			"ContactListName": {Description: descSESListName},
			"Filter":          {Description: "Filter: filtered_status (OPT_IN or OPT_OUT) and topic_filter with topic_name and use_default_if_preference_unavailable"},
		},
		Fields:      map[string]Field{"Contacts": {Description: "Contacts of this page, each with email_address, topic_preferences, topic_default_preferences, unsubscribe_all, and last_updated_timestamp; personal data"}},
		PageSizeMax: 100,
		Examples:    []Example{{Description: "List the contacts that opted in", Arguments: `{"contact_list_name":"newsletter","filter":{"filtered_status":"OPT_IN"}}`}},
	}, "contact"),
	sesContactRead(Operation{
		Operation: "GetContact", Object: "contact", Action: "get",
		Title: "Get an SES contact",
		Description: "Return one contact of a list: topic preferences, the default preferences of the topics, unsubscribe " +
			"state, attributes, and timestamps. The email address and the attributes are personal data and untrusted. Needs ses:GetContact.",
		Arguments: map[string]Argument{
			"ContactListName": {Description: descSESListName},
			"EmailAddress":    {Description: descSESAddress},
		},
		Fields: map[string]Field{
			"ContactListName":         {Description: "Name of the list"},
			"EmailAddress":            {Description: descSESAddress},
			"TopicPreferences":        {Description: "Preferences the contact set"},
			"TopicDefaultPreferences": {Description: "Default preferences of the topics that the contact set none for"},
			"UnsubscribeAll":          {Description: descSESUnsub},
			"AttributesData":          {Description: descSESAttrs},
			"CreatedTimestamp":        {Description: "When the contact was created"},
			"LastUpdatedTimestamp":    {Description: "When the contact was last changed"},
		},
		Limits:   sesAddressLimit,
		Examples: []Example{{Description: "Read a contact", Arguments: `{"contact_list_name":"newsletter","email_address":"<email address of the contact>"}`}},
	}, "contact"),
	sesContactWrite(Operation{
		Operation: "CreateContact", Object: "contact", Action: "create",
		Title: "Add a contact to an SES contact list",
		Description: "Add a contact to a list with optional topic preferences, unsubscribe state, and attributes. The email " +
			"address is personal data. Needs ses:CreateContact. Not repeated after an unclear outcome: read the contact " +
			"before trying again. " + sesWriteRule,
		Arguments: map[string]Argument{
			"ContactListName":  {Description: descSESListName},
			"EmailAddress":     {Description: descSESAddress},
			"TopicPreferences": {Description: descSESTopicPref},
			"UnsubscribeAll":   {Description: descSESUnsub},
			"AttributesData":   {Description: descSESAttrs},
		},
		Limits:   sesAddressLimit,
		Examples: []Example{{Description: "Add a contact that opts in to a topic", Arguments: `{"contact_list_name":"newsletter","email_address":"<email address of the contact>","topic_preferences":[{"topic_name":"news","subscription_status":"OPT_IN"}]}`}},
	}, "create", "non-idempotent", "contact"),
	sesContactWrite(Operation{
		Operation: "UpdateContact", Object: "contact", Action: "update",
		Title: "Update an SES contact",
		Description: "Replace the topic preferences, unsubscribe state, and attributes of a contact; what is left out is reset, " +
			"so give everything to keep. Needs ses:UpdateContact. " + sesWriteRule,
		Arguments: map[string]Argument{
			"ContactListName":  {Description: descSESListName},
			"EmailAddress":     {Description: descSESAddress},
			"TopicPreferences": {Description: descSESTopicPref},
			"UnsubscribeAll":   {Description: descSESUnsub},
			"AttributesData":   {Description: descSESAttrs},
		},
		Limits:   sesAddressLimit,
		Examples: []Example{{Description: "Opt a contact out of everything", Arguments: `{"contact_list_name":"newsletter","email_address":"<email address of the contact>","unsubscribe_all":true}`}},
	}, "update", "idempotent", "contact"),
	sesContactWrite(Operation{
		Operation: "DeleteContact", Object: "contact", Action: "delete",
		Title:       "Remove a contact from an SES contact list",
		Description: "Delete a contact from a list. Needs ses:DeleteContact. Not repeated after an unclear outcome. " + sesWriteRule,
		Arguments: map[string]Argument{
			"ContactListName": {Description: descSESListName},
			"EmailAddress":    {Description: descSESAddress},
		},
		Limits:   sesAddressLimit,
		Examples: []Example{{Description: "Remove a contact", Arguments: `{"contact_list_name":"newsletter","email_address":"<email address of the contact>"}`}},
	}, "delete", "non-idempotent", "contact"),
}
