package googleads

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

// An asset link attaches an asset to a campaign, an ad group, or the account for one field type. The services of
// these families share one form: the resource name is PARENT~ASSET~FIELD_TYPE below the collection of the family,
// only status is mutable, and the answer carries the same status fields. assetLinkKind holds what differs, so a
// family with a parent of another kind reuses the checks and the binding of answers below.

// assetLinkKind describes one family of asset links.
type assetLinkKind struct {
	// collection is the collection of the links below the customer, such as campaignAssets.
	collection string
	// parentCollection is the collection of the parents, such as campaigns, and parentKey the member of the wire
	// object that names the parent, such as campaign. Links of the account have no parent: both stay empty and the
	// resource name is ASSET~FIELD_TYPE.
	parentCollection, parentKey string
	// parentNoun and parentArgument name the parent in error texts, such as campaign and campaign_id.
	parentNoun, parentArgument string
	// resourceKey is the member of a mutate result that holds the link after the change.
	resourceKey string
}

// assetLinksMaxOperations is Qatlas's own bound of the entries of one call; the reference pages of v26 state no
// maximum.
const assetLinksMaxOperations = 50

// assetLinkStatuses are the statuses a request may set. REMOVED is reached by the remove tool alone.
var assetLinkStatuses = []string{"ENABLED", "PAUSED"}

// assetLinkFieldTypes are the field types of the enum of the API, without the two values that are never sent.
// Which field type fits which asset and campaign is up to Google.
var assetLinkFieldTypes = enumNamed("AssetFieldTypeEnum.AssetFieldType")

// assetLinkKey names one link by checked parts.
type assetLinkKey struct {
	ParentID, AssetID, FieldType string
}

// name is the resource name of the link, built from checked parts alone.
func (k assetLinkKind) name(customerID string, key assetLinkKey) string {
	rest := key.AssetID + "~" + key.FieldType
	if k.parentCollection != "" {
		rest = key.ParentID + "~" + rest
	}
	return customerName(customerID) + "/" + k.collection + "/" + rest
}

func (k assetLinkKind) parentName(customerID, parentID string) string {
	return customerName(customerID) + "/" + k.parentCollection + "/" + parentID
}

// parse returns the key of a resource name of a link of the account.
func (k assetLinkKind) parse(name, customerID string) (assetLinkKey, bool) {
	rest, ok := strings.CutPrefix(name, customerName(customerID)+"/"+k.collection+"/")
	if !ok {
		return assetLinkKey{}, false
	}
	parts := strings.Split(rest, "~")
	var key assetLinkKey
	if k.parentCollection != "" {
		if len(parts) != 3 {
			return assetLinkKey{}, false
		}
		key.ParentID, parts = parts[0], parts[1:]
	}
	if len(parts) != 2 {
		return assetLinkKey{}, false
	}
	key.AssetID, key.FieldType = parts[0], parts[1]
	return key, (k.parentCollection == "" || assetLinkIDOK(key.ParentID)) && assetLinkIDOK(key.AssetID) &&
		slices.Contains(assetLinkFieldTypes, key.FieldType)
}

func assetLinkIDOK(value string) bool {
	if !numericIDPattern.MatchString(value) {
		return false
	}
	_, err := strconv.ParseInt(value, 10, 64)
	return err == nil
}

// check refuses a key that is not made of the IDs of existing objects and a field type of the enum; this also
// refuses the temporary negative IDs of a batch.
func (k assetLinkKind) check(path string, key assetLinkKey) error {
	switch {
	case k.parentCollection != "" && !assetLinkIDOK(key.ParentID):
		return badRequest(path + k.parentArgument + " must be the ID of an existing " + k.parentNoun + ", a positive number")
	case !assetLinkIDOK(key.AssetID):
		return badRequest(path + "asset_id must be the ID of an existing asset, a positive number")
	case !slices.Contains(assetLinkFieldTypes, key.FieldType):
		return badRequest(path + "field_type must be the name of an asset field type of the API, such as HEADLINE")
	}
	return nil
}

func assetLinkCheckStatus(path, status string) error {
	if !slices.Contains(assetLinkStatuses, status) {
		return badRequest(path + "status must be ENABLED or PAUSED; a link is removed with the remove tool")
	}
	return nil
}

func assetLinkCheckCount(argument string, n int) error {
	if n < 1 || n > assetLinksMaxOperations {
		return badRequest(argument + " must hold 1 to " + strconv.Itoa(assetLinksMaxOperations) + " entries")
	}
	return nil
}

// --- results ---

type (
	// assetLinkObject is a link after the change as Google returns it, without the parent, which the family names.
	assetLinkObject struct {
		ResourceName         string                `json:"resource_name"`
		Asset                string                `json:"asset,omitempty"`
		FieldType            string                `json:"field_type,omitempty"`
		Status               string                `json:"status,omitempty"`
		Source               string                `json:"source,omitempty"`
		PrimaryStatus        string                `json:"primary_status,omitempty"`
		PrimaryStatusReasons []string              `json:"primary_status_reasons,omitempty"`
		PrimaryStatusDetails []assetLinkStatusInfo `json:"primary_status_details,omitempty"`
	}
	assetLinkStatusInfo struct {
		Status           string                `json:"status,omitempty"`
		Reason           string                `json:"reason,omitempty"`
		AssetDisapproved *assetLinkDisapproval `json:"asset_disapproved,omitempty"`
	}
	assetLinkDisapproval struct {
		OfflineEvaluationErrorReasons []string `json:"offline_evaluation_error_reasons,omitempty"`
	}
	// assetLinkDone is one result of the answer, bound to the account and the key asked for. Parent is the
	// resource name of the parent as the returned object names it, empty without an object.
	assetLinkDone struct {
		Key          assetLinkKey
		ResourceName string
		Parent       string
		Link         *assetLinkObject
	}
)

type assetLinkWire struct {
	ResourceName         string   `json:"resourceName"`
	Asset                string   `json:"asset"`
	FieldType            string   `json:"fieldType"`
	Status               string   `json:"status"`
	Source               string   `json:"source"`
	PrimaryStatus        string   `json:"primaryStatus"`
	PrimaryStatusReasons []string `json:"primaryStatusReasons"`
	PrimaryStatusDetails []struct {
		Status           string `json:"status"`
		Reason           string `json:"reason"`
		AssetDisapproved *struct {
			OfflineEvaluationErrorReasons []string `json:"offlineEvaluationErrorReasons"`
		} `json:"assetDisapproved"`
	} `json:"primaryStatusDetails"`
}

// object reads the returned link and binds it to the key and the account. It is not the link asked for when the
// resource name, the asset, or the field type differ.
func (k assetLinkKind) object(raw json.RawMessage, customerID, name string, key assetLinkKey) (*assetLinkObject, string, bool) {
	var w assetLinkWire
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &w) != nil || json.Unmarshal(raw, &members) != nil || w.ResourceName != name {
		return nil, "", false
	}
	var parent string
	if p, ok := members[k.parentKey]; ok && json.Unmarshal(p, &parent) != nil {
		return nil, "", false
	}
	if parent != "" && parent != k.parentName(customerID, key.ParentID) {
		return nil, "", false
	}
	if w.Asset != "" && w.Asset != customerName(customerID)+"/assets/"+key.AssetID {
		return nil, "", false
	}
	if w.FieldType != "" && w.FieldType != key.FieldType {
		return nil, "", false
	}
	out := &assetLinkObject{ResourceName: name, Asset: w.Asset, FieldType: w.FieldType, Status: code(w.Status),
		Source: code(w.Source), PrimaryStatus: code(w.PrimaryStatus),
		PrimaryStatusReasons: boundedList(w.PrimaryStatusReasons, maxCodeText)}
	if len(out.PrimaryStatusReasons) == 0 {
		out.PrimaryStatusReasons = nil
	}
	for _, d := range w.PrimaryStatusDetails[:min(len(w.PrimaryStatusDetails), maxListItems)] {
		info := assetLinkStatusInfo{Status: code(d.Status), Reason: code(d.Reason)}
		if d.AssetDisapproved != nil {
			info.AssetDisapproved = &assetLinkDisapproval{
				OfflineEvaluationErrorReasons: boundedList(d.AssetDisapproved.OfflineEvaluationErrorReasons, maxCodeText)}
		}
		out.PrimaryStatusDetails = append(out.PrimaryStatusDetails, info)
	}
	return out, parent, true
}

// results binds the answer to the checked account and to the links asked for: exactly these, in this order.
// Anything else leaves the outcome open, since the request was sent.
func (k assetLinkKind) results(op, customerID string, results []map[string]json.RawMessage, partialFailure json.RawMessage,
	want []assetLinkKey, validateOnly bool) ([]assetLinkDone, error) {
	if len(partialFailure) > 0 && string(partialFailure) != "null" {
		return nil, unsureResult(op, "answered with a partial failure although the request asked for none")
	}
	out := []assetLinkDone{}
	if validateOnly && len(results) == 0 {
		return out, nil
	}
	if len(results) != len(want) {
		return nil, unsureResult(op, "answered with another number of results than the operations asked for")
	}
	for i, r := range results {
		var name string
		if json.Unmarshal(r["resourceName"], &name) != nil || name != k.name(customerID, want[i]) {
			return nil, unsureResult(op, "answered with a result for another link than the one asked for")
		}
		done := assetLinkDone{Key: want[i], ResourceName: name}
		if raw := r[k.resourceKey]; len(raw) > 0 && string(raw) != "null" {
			link, parent, ok := k.object(raw, customerID, name, want[i])
			if !ok {
				return nil, unsureResult(op, "answered with a link that is not the one asked for")
			}
			done.Link, done.Parent = link, parent
		}
		out = append(out, done)
	}
	return out, nil
}

// --- confirmation ---

func assetLinksLead(validateOnly bool) string {
	if validateOnly {
		return "Only validate: "
	}
	return ""
}

// assetLinksList phrases the entries for the confirmation, at most ten, from the arguments alone and with
// every text bounded.
func assetLinksList(n int, entry func(i int) string) string {
	shown := make([]string, 0, 10)
	for i := 0; i < min(n, 10); i++ {
		shown = append(shown, entry(i))
	}
	more := ""
	if n > len(shown) {
		more = " and " + strconv.Itoa(n-len(shown)) + " more"
	}
	return strings.Join(shown, "; ") + more
}
