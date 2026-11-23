package googlemerchant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	shippingSettingsGetID    = Provider + ".shipping-settings.get"
	shippingSettingsInsertID = Provider + ".shipping-settings.insert"
)

// The ShippingSettings resource documents no count or length limit. These bounds are ours: they keep one
// call and one answer small and are far above what a shop needs.
const (
	maxShipServices    = 50
	maxShipWarehouses  = 50
	maxShipCountries   = 250
	maxShipGroups      = 50
	maxShipLabels      = 100
	maxShipLines       = 200
	maxShipStoreCodes  = 1000
	maxShipSmall       = 20
	maxShipTextRunes   = 200
	maxShipStreetRunes = 300
	maxShipBody        = 1 << 20
)

// number is an int64 member. Google writes int64 as a string and accepts both forms; a result shows a number.
type number int64

func (n *number) UnmarshalJSON(b []byte) error {
	var text string
	if json.Unmarshal(b, &text) != nil {
		text = string(bytes.TrimSpace(b))
	}
	v, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return errors.New("not an integer")
	}
	*n = number(v)
	return nil
}

type (
	shipPrice struct {
		AmountMicros string `json:"amountMicros,omitempty"`
		CurrencyCode string `json:"currencyCode,omitempty"`
	}
	shipWeight struct {
		AmountMicros string `json:"amountMicros,omitempty"`
		Unit         string `json:"unit,omitempty"`
	}
	shipValue struct {
		NoShipping      *bool      `json:"noShipping,omitempty"`
		FlatRate        *shipPrice `json:"flatRate,omitempty"`
		PricePercentage string     `json:"pricePercentage,omitempty"`
		CarrierRate     string     `json:"carrierRate,omitempty"`
		Subtable        string     `json:"subtable,omitempty"`
	}
	shipLocationSet struct {
		LocationIDs []string `json:"locationIds,omitempty"`
	}
	shipHeaders struct {
		Prices               []shipPrice       `json:"prices,omitempty"`
		Weights              []shipWeight      `json:"weights,omitempty"`
		NumberOfItems        []string          `json:"numberOfItems,omitempty"`
		PostalCodeGroupNames []string          `json:"postalCodeGroupNames,omitempty"`
		Locations            []shipLocationSet `json:"locations,omitempty"`
	}
	shipRow struct {
		Cells []shipValue `json:"cells,omitempty"`
	}
	shipTable struct {
		Name          string       `json:"name,omitempty"`
		RowHeaders    *shipHeaders `json:"rowHeaders,omitempty"`
		ColumnHeaders *shipHeaders `json:"columnHeaders,omitempty"`
		Rows          []shipRow    `json:"rows,omitempty"`
	}
	shipCarrierRate struct {
		Name                 string     `json:"name,omitempty"`
		Carrier              string     `json:"carrier,omitempty"`
		CarrierService       string     `json:"carrierService,omitempty"`
		OriginPostalCode     string     `json:"originPostalCode,omitempty"`
		PercentageAdjustment string     `json:"percentageAdjustment,omitempty"`
		FlatAdjustment       *shipPrice `json:"flatAdjustment,omitempty"`
	}
	shipRateGroup struct {
		ApplicableShippingLabels []string          `json:"applicableShippingLabels,omitempty"`
		Subtables                []shipTable       `json:"subtables,omitempty"`
		CarrierRates             []shipCarrierRate `json:"carrierRates,omitempty"`
		SingleValue              *shipValue        `json:"singleValue,omitempty"`
		MainTable                *shipTable        `json:"mainTable,omitempty"`
		Name                     string            `json:"name,omitempty"`
	}
	shipTier struct {
		TierLabel string `json:"tierLabel,omitempty"`
	}
	shipLoyalty struct {
		LoyaltyProgramTiers []shipTier `json:"loyaltyProgramTiers,omitempty"`
		ProgramLabel        string     `json:"programLabel,omitempty"`
	}
	shipBusinessDays struct {
		BusinessDays []string `json:"businessDays,omitempty"`
	}
	shipCutoff struct {
		Hour     *number `json:"hour,omitempty"`
		Minute   *number `json:"minute,omitempty"`
		TimeZone string  `json:"timeZone,omitempty"`
	}
	shipHourMinute struct {
		Hour   *number `json:"hour,omitempty"`
		Minute *number `json:"minute,omitempty"`
	}
	shipTransitValue struct {
		MinTransitDays *number `json:"minTransitDays,omitempty"`
		MaxTransitDays *number `json:"maxTransitDays,omitempty"`
	}
	shipTransitRow struct {
		Values []shipTransitValue `json:"values,omitempty"`
	}
	shipTransitTable struct {
		PostalCodeGroupNames []string         `json:"postalCodeGroupNames,omitempty"`
		TransitTimeLabels    []string         `json:"transitTimeLabels,omitempty"`
		Rows                 []shipTransitRow `json:"rows,omitempty"`
	}
	shipWarehouseTime struct {
		Carrier        string `json:"carrier,omitempty"`
		CarrierService string `json:"carrierService,omitempty"`
		Warehouse      string `json:"warehouse,omitempty"`
	}
	shipDeliveryTime struct {
		WarehouseBasedDeliveryTimes []shipWarehouseTime `json:"warehouseBasedDeliveryTimes,omitempty"`
		MinTransitDays              *number             `json:"minTransitDays,omitempty"`
		MaxTransitDays              *number             `json:"maxTransitDays,omitempty"`
		CutoffTime                  *shipCutoff         `json:"cutoffTime,omitempty"`
		MinHandlingDays             *number             `json:"minHandlingDays,omitempty"`
		MaxHandlingDays             *number             `json:"maxHandlingDays,omitempty"`
		TransitTimeTable            *shipTransitTable   `json:"transitTimeTable,omitempty"`
		HandlingBusinessDayConfig   *shipBusinessDays   `json:"handlingBusinessDayConfig,omitempty"`
		TransitBusinessDayConfig    *shipBusinessDays   `json:"transitBusinessDayConfig,omitempty"`
	}
	shipStoreCodeSet struct {
		StoreCodes []string   `json:"storeCodes,omitempty"`
		Value      *shipPrice `json:"value,omitempty"`
	}
	shipMinimumTable struct {
		StoreCodeSetWithMovs []shipStoreCodeSet `json:"storeCodeSetWithMovs,omitempty"`
	}
	shipCutoffConfig struct {
		LocalCutoffTime       *shipHourMinute `json:"localCutoffTime,omitempty"`
		StoreCloseOffsetHours *number         `json:"storeCloseOffsetHours,omitempty"`
		NoDeliveryPostCutoff  *bool           `json:"noDeliveryPostCutoff,omitempty"`
	}
	shipDistance struct {
		Value *number `json:"value,omitempty"`
		Unit  string  `json:"unit,omitempty"`
	}
	shipStoreConfig struct {
		StoreCodes       []string          `json:"storeCodes,omitempty"`
		StoreServiceType string            `json:"storeServiceType,omitempty"`
		CutoffConfig     *shipCutoffConfig `json:"cutoffConfig,omitempty"`
		ServiceRadius    *shipDistance     `json:"serviceRadius,omitempty"`
	}
	shipService struct {
		DeliveryCountries      []string          `json:"deliveryCountries,omitempty"`
		RateGroups             []shipRateGroup   `json:"rateGroups,omitempty"`
		LoyaltyPrograms        []shipLoyalty     `json:"loyaltyPrograms,omitempty"`
		ServiceName            string            `json:"serviceName,omitempty"`
		Active                 *bool             `json:"active,omitempty"`
		CurrencyCode           string            `json:"currencyCode,omitempty"`
		DeliveryTime           *shipDeliveryTime `json:"deliveryTime,omitempty"`
		ShipmentType           string            `json:"shipmentType,omitempty"`
		MinimumOrderValue      *shipPrice        `json:"minimumOrderValue,omitempty"`
		MinimumOrderValueTable *shipMinimumTable `json:"minimumOrderValueTable,omitempty"`
		StoreConfig            *shipStoreConfig  `json:"storeConfig,omitempty"`
	}
	shipAddress struct {
		StreetAddress      string `json:"streetAddress,omitempty"`
		City               string `json:"city,omitempty"`
		AdministrativeArea string `json:"administrativeArea,omitempty"`
		PostalCode         string `json:"postalCode,omitempty"`
		RegionCode         string `json:"regionCode,omitempty"`
	}
	shipWarehouse struct {
		Name              string            `json:"name,omitempty"`
		ShippingAddress   *shipAddress      `json:"shippingAddress,omitempty"`
		CutoffTime        *shipHourMinute   `json:"cutoffTime,omitempty"`
		HandlingDays      *number           `json:"handlingDays,omitempty"`
		BusinessDayConfig *shipBusinessDays `json:"businessDayConfig,omitempty"`
	}
)

// shippingSettings is the ShippingSettings resource of a result. name is output only.
type shippingSettings struct {
	Name       idText          `json:"name,omitempty"`
	Services   []shipService   `json:"services,omitempty"`
	Warehouses []shipWarehouse `json:"warehouses,omitempty"`
	Etag       string          `json:"etag,omitempty"`
}

// shippingBody is the whole ShippingSettings as sent: insert replaces the setting with exactly this. The
// etag is always sent, empty for a first insert.
type shippingBody struct {
	Services   []shipService   `json:"services,omitempty"`
	Warehouses []shipWarehouse `json:"warehouses,omitempty"`
	Etag       string          `json:"etag"`
}

// clipTree cuts every text of a decoded answer and caps every list: the answer is untrusted content.
func clipTree(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			clipTree(v.Elem())
		}
	case reflect.Struct:
		for i := range v.NumField() {
			clipTree(v.Field(i))
		}
	case reflect.Slice:
		if v.Len() > maxEntries {
			v.Set(v.Slice(0, maxEntries))
		}
		for i := range v.Len() {
			clipTree(v.Index(i))
		}
	case reflect.String:
		v.SetString(clipText(v.String(), maxNameRunes))
	}
}

var (
	shipNumber  = `{"type":"integer"}`
	shipText    = `{"type":"string","minLength":1,"maxLength":` + strconv.Itoa(maxShipTextRunes) + `}`
	shipDecimal = `{"type":"string","pattern":"^-?[0-9]{1,12}(\\.[0-9]{1,9})?$"}`
	shipTexts   = func(max int) string {
		return `{"type":"array","minItems":1,"maxItems":` + strconv.Itoa(max) + `,"items":` + shipText + `}`
	}
	shipList = func(item string, max int) string {
		return `{"type":"array","maxItems":` + strconv.Itoa(max) + `,"items":` + item + `}`
	}
	shipPriceSchema = closedSchemaOf([]string{"amountMicros", "currencyCode"},
		member("amountMicros", `{"type":"string","pattern":"^-?[0-9]{1,18}$"}`),
		member("currencyCode", `{"type":"string","pattern":"^[A-Z]{3}$"}`))
	shipWeightSchema = closedSchemaOf([]string{"amountMicros", "unit"},
		member("amountMicros", `{"type":"string","pattern":"^-?[0-9]{1,18}$"}`),
		member("unit", `{"type":"string","enum":["POUND","KILOGRAM"]}`))
	shipValueSchema = `{"type":"object","properties":{` + strings.Join([]string{
		member("noShipping", `{"type":"boolean","const":true}`), member("flatRate", shipPriceSchema),
		member("pricePercentage", shipDecimal), member("carrierRate", shipText), member("subtable", shipText)}, ",") +
		`},"minProperties":1,"maxProperties":1,"additionalProperties":false}`
	shipHeadersSchema = `{"type":"object","properties":{` + strings.Join([]string{
		member("prices", shipList(shipPriceSchema, maxShipLines)),
		member("weights", shipList(shipWeightSchema, maxShipLines)),
		member("numberOfItems", shipList(shipText, maxShipLines)),
		member("postalCodeGroupNames", shipList(shipText, maxShipLines)),
		member("locations", shipList(closedSchemaOf([]string{"locationIds"},
			member("locationIds", shipTexts(maxShipLines))), maxShipLines))}, ",") +
		`},"minProperties":1,"maxProperties":1,"additionalProperties":false}`
	shipTableSchema = closedSchemaOf([]string{"rowHeaders", "rows"}, member("name", shipText),
		member("rowHeaders", shipHeadersSchema), member("columnHeaders", shipHeadersSchema),
		member("rows", `{"type":"array","minItems":1,"maxItems":`+strconv.Itoa(maxShipLines)+`,"items":`+
			closedSchemaOf([]string{"cells"}, member("cells", `{"type":"array","minItems":1,"maxItems":`+
				strconv.Itoa(maxShipLines)+`,"items":`+shipValueSchema+`}`))+`}`))
	shipCarrierRateSchema = closedSchemaOf([]string{"name", "carrier", "carrierService", "originPostalCode"},
		member("name", shipText), member("carrier", shipText), member("carrierService", shipText),
		member("originPostalCode", shipText), member("percentageAdjustment", shipDecimal),
		member("flatAdjustment", shipPriceSchema))
	shipRateGroupSchema = closedSchemaOf(nil, member("applicableShippingLabels", shipList(shipText, maxShipLabels)),
		member("subtables", shipList(shipTableSchema, maxShipGroups)),
		member("carrierRates", shipList(shipCarrierRateSchema, maxShipGroups)),
		member("singleValue", shipValueSchema), member("mainTable", shipTableSchema), member("name", shipText))
	shipDaysSchema = closedSchemaOf([]string{"businessDays"}, member("businessDays",
		`{"type":"array","minItems":1,"maxItems":7,"uniqueItems":true,"items":{"type":"string","enum":[`+
			`"MONDAY","TUESDAY","WEDNESDAY","THURSDAY","FRIDAY","SATURDAY","SUNDAY"]}}`))
	shipHourMinuteSchema = closedSchemaOf([]string{"hour", "minute"}, member("hour", `{"type":"integer","minimum":0,"maximum":23}`),
		member("minute", `{"type":"integer","minimum":0,"maximum":59}`))
	shipDaysCount          = `{"type":"integer","minimum":0,"maximum":365}`
	shipDeliveryTimeSchema = closedSchemaOf(nil,
		member("warehouseBasedDeliveryTimes", shipList(closedSchemaOf([]string{"carrier", "carrierService", "warehouse"},
			member("carrier", shipText), member("carrierService", shipText), member("warehouse", shipText)), maxShipSmall)),
		member("minTransitDays", shipDaysCount), member("maxTransitDays", shipDaysCount),
		member("cutoffTime", closedSchemaOf([]string{"hour", "minute", "timeZone"},
			member("hour", `{"type":"integer","minimum":0,"maximum":23}`),
			member("minute", `{"type":"integer","minimum":0,"maximum":59}`), member("timeZone", shipText))),
		member("minHandlingDays", shipDaysCount), member("maxHandlingDays", shipDaysCount),
		member("transitTimeTable", closedSchemaOf([]string{"rows"},
			member("postalCodeGroupNames", shipList(shipText, maxShipLines)),
			member("transitTimeLabels", shipList(shipText, maxShipLines)),
			member("rows", `{"type":"array","minItems":1,"maxItems":`+strconv.Itoa(maxShipLines)+`,"items":`+
				closedSchemaOf([]string{"values"}, member("values", `{"type":"array","minItems":1,"maxItems":`+
					strconv.Itoa(maxShipLines)+`,"items":`+closedSchemaOf(nil, member("minTransitDays", shipDaysCount),
					member("maxTransitDays", shipDaysCount))+`}`))+`}`))),
		member("handlingBusinessDayConfig", shipDaysSchema), member("transitBusinessDayConfig", shipDaysSchema))
	shipStoreConfigSchema = closedSchemaOf([]string{"storeServiceType"},
		member("storeCodes", shipList(shipText, maxShipStoreCodes)),
		member("storeServiceType", `{"type":"string","enum":["ALL_STORES","SELECTED_STORES"]}`),
		member("cutoffConfig", closedSchemaOf(nil, member("localCutoffTime", shipHourMinuteSchema),
			member("storeCloseOffsetHours", `{"type":"integer","minimum":0,"maximum":24}`),
			member("noDeliveryPostCutoff", boolSchema))),
		member("serviceRadius", closedSchemaOf([]string{"value", "unit"},
			member("value", `{"type":"integer","minimum":1,"maximum":100000}`),
			member("unit", `{"type":"string","enum":["MILES","KILOMETERS"]}`))))
	shipServiceSchema = closedSchemaOf([]string{"deliveryCountries", "serviceName", "active", "currencyCode", "deliveryTime"},
		member("deliveryCountries", `{"type":"array","minItems":1,"maxItems":`+strconv.Itoa(maxShipCountries)+
			`,"uniqueItems":true,"items":{"type":"string","pattern":"^[A-Z]{2}$"}}`),
		member("rateGroups", shipList(shipRateGroupSchema, maxShipGroups)),
		member("loyaltyPrograms", shipList(closedSchemaOf(nil, member("programLabel", shipText),
			member("loyaltyProgramTiers", shipList(closedSchemaOf(nil, member("tierLabel", shipText)), maxShipSmall))),
			maxShipSmall)),
		member("serviceName", shipText), member("active", boolSchema),
		member("currencyCode", `{"type":"string","pattern":"^[A-Z]{3}$"}`),
		member("deliveryTime", shipDeliveryTimeSchema),
		member("shipmentType", `{"type":"string","enum":["DELIVERY","LOCAL_DELIVERY","COLLECTION_POINT"]}`),
		member("minimumOrderValue", shipPriceSchema),
		member("minimumOrderValueTable", closedSchemaOf([]string{"storeCodeSetWithMovs"}, member("storeCodeSetWithMovs",
			`{"type":"array","minItems":2,"maxItems":`+strconv.Itoa(maxShipSmall*5)+`,"items":`+
				closedSchemaOf([]string{"value"}, member("storeCodes", shipList(shipText, maxShipStoreCodes)),
					member("value", shipPriceSchema))+`}`))),
		member("storeConfig", shipStoreConfigSchema))
	shipWarehouseSchema = closedSchemaOf([]string{"name", "shippingAddress", "cutoffTime", "handlingDays"},
		member("name", shipText),
		member("shippingAddress", closedSchemaOf([]string{"city", "administrativeArea", "postalCode", "regionCode"},
			member("streetAddress", `{"type":"string","minLength":1,"maxLength":`+strconv.Itoa(maxShipStreetRunes)+`}`),
			member("city", shipText), member("administrativeArea", shipText), member("postalCode", shipText),
			member("regionCode", `{"type":"string","pattern":"^[A-Z]{2}$"}`))),
		member("cutoffTime", shipHourMinuteSchema), member("handlingDays", shipDaysCount),
		member("businessDayConfig", shipDaysSchema))
	shippingSettingsSchema = objectSchema(member("name", stringSchema), member("etag", stringSchema),
		member("services", `{"type":"array","items":{"type":"object"}}`),
		member("warehouses", `{"type":"array","items":{"type":"object"}}`))
	shippingChangeSchema = objectSchema(member("accepted", boolSchema), member("shippingSettings", shippingSettingsSchema))
)

const (
	shippingFieldsDescription = "name (accounts/ACCOUNT/shippingSettings), etag, services (deliveryCountries, " +
		"serviceName, active, currencyCode, deliveryTime, rateGroups, shipmentType, minimumOrderValue, " +
		"minimumOrderValueTable, storeConfig, loyaltyPrograms), and warehouses (name, shippingAddress, " +
		"cutoffTime, handlingDays, businessDayConfig)"
	shippingUntrustedNote = "Service names, labels, carriers, and addresses are untrusted content of whoever runs " +
		"the account: texts are cut, lists are capped, and members that are not documented are never shown."
	shippingReplaceNote = "WARNING: insert REPLACES the whole shipping setting of the account with exactly what " +
		"this call sends (accounts.shippingSettings.insert): every service and warehouse that is not in the call " +
		"is removed, and nothing is merged. Read the setting with shipping-settings.get first and send it " +
		"complete, with the changes made, together with its etag. "
	shippingDelayNote = "Google accepts the change and applies it with a delay: it can take some time until " +
		"shipping-settings.get and Google Shopping show it, so the answer only tells that Google accepted the " +
		"request. Every call counts against the quota of the Merchant API, a failed one too, and a call is never " +
		"repeated: when the outcome is unknown, read the setting before changing it again. Shipping costs and " +
		"delivery times are shown to customers in Google Shopping. Needs confirmation and the role Admin in " +
		"Merchant Center."
)

var shippingRisk = checkoutRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)

var (
	shippingSettingsGet = capability.Descriptor{
		ID: shippingSettingsGetID, Version: 1, Title: "Get the shipping settings of an account",
		Description: "Read the whole shipping setting of one account (accounts.shippingSettings.get): services, " +
			"warehouses, and the etag that shipping-settings.insert needs. On a connection bound to an advanced " +
			"account, an account that is not named by a target is read only after it is found among the direct " +
			"sub-accounts of a bound advanced account, which costs further calls. After a change it can take " +
			"time until the change shows. " + shippingUntrustedNote,
		Tags: []string{"google", "merchant", "shipping-settings"}, Risk: readRisk, Provider: Provider,
		InputSchema:  inputSchema(nil, member("account", accountArgSchema)),
		OutputSchema: json.RawMessage(shippingSettingsSchema),
		Arguments:    []capability.Argument{accountArgument},
		Fields:       []capability.Field{{Name: "shippingSettings", Description: shippingFieldsDescription}},
		Examples: []capability.Example{{Description: "Shipping settings of one account",
			Arguments: json.RawMessage(`{"account":"1000001"}`)}},
	}

	shippingSettingsInsert = capability.Descriptor{
		ID: shippingSettingsInsertID, Version: 1, Title: "Replace the shipping settings of an account",
		Description: shippingReplaceNote + "The arguments are the whole setting: etag (required; the etag of " +
			"shipping-settings.get, or an empty string for the first insert; Google refuses the call when the " +
			"setting changed since), services (required, 1 to 50; each with deliveryCountries, serviceName, " +
			"active, currencyCode, deliveryTime, and optionally rateGroups, shipmentType, minimumOrderValue or " +
			"minimumOrderValueTable, storeConfig, loyaltyPrograms), and warehouses (optional, up to 50). Members " +
			"are written as Google names them. Qatlas checks forms, required members, enums, exclusive members, " +
			"and the references inside one service before any request; Google checks the rest. The counts and " +
			"lengths are Qatlas' own bounds. Offered only where a connection's tools list names it. " +
			shippingDelayNote + " " + shippingUntrustedNote,
		Tags: []string{"google", "merchant", "shipping-settings"}, Provider: Provider, RequiresToolAllowList: true,
		Risk: shippingRisk,
		InputSchema: inputSchema([]string{"etag", "services"}, member("account", accountArgSchema),
			member("etag", `{"type":"string","maxLength":500}`),
			member("services", `{"type":"array","minItems":1,"maxItems":`+strconv.Itoa(maxShipServices)+`,"items":`+
				shipServiceSchema+`}`),
			member("warehouses", shipList(shipWarehouseSchema, maxShipWarehouses))),
		OutputSchema: json.RawMessage(shippingChangeSchema),
		Arguments: []capability.Argument{checkoutAccountArgument,
			{Name: "etag", Required: true, Description: "etag from shipping-settings.get, or an empty string when the " +
				"account has no shipping setting yet"},
			{Name: "services", Required: true, Description: "ALL services of the new setting; services left out are removed"},
			{Name: "warehouses", Description: "ALL warehouses of the new setting; warehouses left out are removed"}},
		Fields: []capability.Field{
			{Name: "accepted", Description: "true when Google accepted the request; nothing is said about processing"},
			{Name: "shippingSettings", Description: "The setting as Google answered: " + shippingFieldsDescription}},
		Examples: []capability.Example{{Description: "Replace the setting with one flat-rate service",
			Arguments: json.RawMessage(`{"account":"1000001","etag":"","services":[{"deliveryCountries":["DE"],` +
				`"serviceName":"Standard","active":true,"currencyCode":"EUR","deliveryTime":{"minTransitDays":2,` +
				`"maxTransitDays":4,"minHandlingDays":0,"maxHandlingDays":1,"cutoffTime":{"hour":14,"minute":0,` +
				`"timeZone":"Europe/Berlin"}},"rateGroups":[{"singleValue":{"flatRate":{"amountMicros":"4900000",` +
				`"currencyCode":"EUR"}}}]}]}`)}},
	}
)

func shippingDescriptors() []capability.Descriptor {
	return []capability.Descriptor{shippingSettingsGet, shippingSettingsInsert}
}

func shippingHandlers() map[string]capability.Handler {
	return map[string]capability.Handler{shippingSettingsGetID: handleShippingGet,
		shippingSettingsInsertID: handleShippingInsert}
}

type shippingArguments struct {
	Account    string          `json:"account"`
	Etag       *string         `json:"etag"`
	Services   []shipService   `json:"services"`
	Warehouses []shipWarehouse `json:"warehouses"`
}

// readShippingArguments refuses a member that is not documented, so nothing unknown is sent to Google.
func readShippingArguments(raw json.RawMessage) (shippingArguments, error) {
	var a shippingArguments
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return a, invalidArgument("the arguments are not in the documented form: " + shippingDecodeHint(err))
	}
	return a, nil
}

// shippingDecodeHint names the member of a decode error without echoing the value.
func shippingDecodeHint(err error) string {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Field != "" {
		return typeErr.Field + " has the wrong type"
	}
	if strings.HasPrefix(err.Error(), "json: unknown field") {
		return err.Error()
	}
	return "unreadable JSON"
}

func shippingPath(id string) string { return accountPath(id) + "/shippingSettings" }

func handleShippingGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	c, err := prepare("get shipping settings", shippingSettingsGetID, resolved, raw)
	if err != nil {
		return nil, err
	}
	id, needsProof, err := c.scope.accountOf(c.args.Account)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if needsProof {
		if err := cl.prove(ctx, c.op, c.tool, id); err != nil {
			return nil, err
		}
	}
	var out shippingSettings
	if err := cl.get(ctx, c.op, c.tool, shippingPath(id), nil, &out); err != nil {
		return nil, err
	}
	clipTree(reflect.ValueOf(&out))
	return out, nil
}

type shippingChange struct {
	Accepted         bool             `json:"accepted"`
	ShippingSettings shippingSettings `json:"shippingSettings"`
}

func handleShippingInsert(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	c, err := prepare("insert shipping settings", shippingSettingsInsertID, resolved, raw)
	if err != nil {
		return nil, err
	}
	a, err := readShippingArguments(raw)
	if err != nil {
		return nil, err
	}
	if a.Account == "" {
		return nil, invalidArgument("account is required")
	}
	if err := a.check(); err != nil {
		return nil, err
	}
	id, needsProof, err := c.scope.accountOf(a.Account)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(shippingBody{Services: a.Services, Warehouses: a.Warehouses, Etag: *a.Etag})
	if err != nil {
		return nil, provider.Fail(c.op, "the request could not be built")
	}
	if len(body) > maxShipBody {
		return nil, invalidArgument("the shipping setting is larger than " + strconv.Itoa(maxShipBody) + " bytes")
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if needsProof {
		if err := cl.prove(ctx, c.op, c.tool, id); err != nil {
			return nil, err
		}
	}
	var out shippingSettings
	r := request{method: http.MethodPost, path: shippingPath(id) + ":insert", body: body, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, &out); err != nil {
		return nil, err
	}
	clipTree(reflect.ValueOf(&out))
	return shippingChange{Accepted: true, ShippingSettings: out}, nil
}

// shippingSummaries phrase what a confirmed call would do, from its arguments alone.
func shippingSummaries() map[string]capability.SummaryFunc {
	return map[string]capability.SummaryFunc{
		shippingSettingsInsertID: func(raw json.RawMessage) string {
			var a shippingArguments
			if json.Unmarshal(raw, &a) != nil {
				return ""
			}
			var names []string
			for _, s := range a.Services {
				names = append(names, strconv.Quote(clipText(s.ServiceName, 60))+" ("+
					clipText(strings.Join(s.DeliveryCountries, ","), 60)+")")
			}
			return "REPLACE the entire shipping setting of Merchant Center account " + a.Account + " with the " +
				strconv.Itoa(len(a.Services)) + " service(s) " + strings.Join(names, ", ") + " and " +
				strconv.Itoa(len(a.Warehouses)) + " warehouse(s) of this call; every existing service and " +
				"warehouse that is not in the call is removed and customers see the new shipping costs and " +
				"delivery times"
		},
	}
}

// Checks of the arguments. Every one runs before any secret is read; Google's own rules for the rest of the
// setting (for example label overlap and supported carriers) are not repeated.

var (
	shipTerritory = regexp.MustCompile(`^[A-Z]{2}$`)
	shipCurrency  = regexp.MustCompile(`^[A-Z]{3}$`)
	shipMicros    = regexp.MustCompile(`^-?[0-9]{1,18}$`)
	shipDecimalRe = regexp.MustCompile(`^-?[0-9]{1,12}(\.[0-9]{1,9})?$`)
	shipDigits    = regexp.MustCompile(`^[0-9]{1,9}$`)
	shipWeekdays  = []string{"MONDAY", "TUESDAY", "WEDNESDAY", "THURSDAY", "FRIDAY", "SATURDAY", "SUNDAY"}
)

func shipBad(name, rule string) error { return invalidArgument(name + " " + rule) }

// shipOneText checks a required text: 1 to max runes, no control characters.
func shipOneText(name, s string, max int) error {
	if n := utf8.RuneCountInString(s); n == 0 || n > max || !utf8.ValidString(s) ||
		strings.ContainsFunc(s, unicode.IsControl) {
		return shipBad(name, "must be 1 to "+strconv.Itoa(max)+" characters without control characters")
	}
	return nil
}

func shipTextList(name string, list []string, min, max int) error {
	if len(list) < min || len(list) > max {
		return shipBad(name, "must hold "+strconv.Itoa(min)+" to "+strconv.Itoa(max)+" entries")
	}
	for _, s := range list {
		if err := shipOneText(name, s, maxShipTextRunes); err != nil {
			return err
		}
	}
	return nil
}

func shipDays(name string, n *number, required bool) error {
	if n == nil {
		if required {
			return shipBad(name, "is required")
		}
		return nil
	}
	if *n < 0 || *n > 365 {
		return shipBad(name, "must be a whole number of days from 0 to 365")
	}
	return nil
}

func shipClock(name string, hour, minute *number) error {
	if hour == nil || minute == nil || *hour < 0 || *hour > 23 || *minute < 0 || *minute > 59 {
		return shipBad(name, "needs hour from 0 to 23 and minute from 0 to 59")
	}
	return nil
}

// shipMoney checks a price; negative allows an adjustment, and currency, when set, is the one of the service.
func shipMoney(name string, p *shipPrice, negative bool, currency string) error {
	if p == nil || !shipMicros.MatchString(p.AmountMicros) || !shipCurrency.MatchString(p.CurrencyCode) {
		return shipBad(name, "needs amountMicros as digits and a three-letter upper case currencyCode")
	}
	if !negative && strings.HasPrefix(p.AmountMicros, "-") {
		return shipBad(name, "must not be negative")
	}
	if currency != "" && p.CurrencyCode != currency {
		return shipBad(name, "must use the currencyCode of its service")
	}
	return nil
}

func checkBusinessDays(name string, c *shipBusinessDays) error {
	if c == nil {
		return nil
	}
	if len(c.BusinessDays) == 0 || len(c.BusinessDays) > len(shipWeekdays) {
		return shipBad(name, "needs 1 to 7 businessDays")
	}
	seen := map[string]bool{}
	for _, d := range c.BusinessDays {
		if !slices.Contains(shipWeekdays, d) || seen[d] {
			return shipBad(name, "businessDays must be MONDAY to SUNDAY, each once")
		}
		seen[d] = true
	}
	return nil
}

func (a shippingArguments) check() error {
	if a.Etag == nil || len(*a.Etag) > 500 || strings.ContainsFunc(*a.Etag, unicode.IsControl) {
		return invalidArgument("etag is required: the etag of shipping-settings.get, or an empty string for the " +
			"first insert")
	}
	if len(a.Services) == 0 || len(a.Services) > maxShipServices {
		return invalidArgument("services must hold 1 to " + strconv.Itoa(maxShipServices) + " services; the call " +
			"replaces the whole setting, so list every service to keep")
	}
	if len(a.Warehouses) > maxShipWarehouses {
		return invalidArgument("warehouses holds at most " + strconv.Itoa(maxShipWarehouses) + " entries")
	}
	names := map[string]bool{}
	for i, s := range a.Services {
		name := "services[" + strconv.Itoa(i) + "]"
		if err := s.check(name); err != nil {
			return err
		}
		if names[s.ServiceName] {
			return shipBad(name+".serviceName", "must be unique in the setting")
		}
		names[s.ServiceName] = true
	}
	houses := map[string]bool{}
	for i, w := range a.Warehouses {
		name := "warehouses[" + strconv.Itoa(i) + "]"
		if err := w.check(name); err != nil {
			return err
		}
		if houses[w.Name] {
			return shipBad(name+".name", "must be unique in the setting")
		}
		houses[w.Name] = true
	}
	return nil
}

func (w shipWarehouse) check(name string) error {
	if err := shipOneText(name+".name", w.Name, maxShipTextRunes); err != nil {
		return err
	}
	ad := w.ShippingAddress
	if ad == nil {
		return shipBad(name+".shippingAddress", "is required")
	}
	for field, v := range map[string]string{"city": ad.City, "administrativeArea": ad.AdministrativeArea,
		"postalCode": ad.PostalCode} {
		if err := shipOneText(name+".shippingAddress."+field, v, maxShipTextRunes); err != nil {
			return err
		}
	}
	if ad.StreetAddress != "" {
		if err := shipOneText(name+".shippingAddress.streetAddress", ad.StreetAddress, maxShipStreetRunes); err != nil {
			return err
		}
	}
	if !shipTerritory.MatchString(ad.RegionCode) {
		return shipBad(name+".shippingAddress.regionCode", "must be a two-letter upper case territory code")
	}
	if w.CutoffTime == nil {
		return shipBad(name+".cutoffTime", "is required")
	}
	if err := shipClock(name+".cutoffTime", w.CutoffTime.Hour, w.CutoffTime.Minute); err != nil {
		return err
	}
	if err := shipDays(name+".handlingDays", w.HandlingDays, true); err != nil {
		return err
	}
	return checkBusinessDays(name+".businessDayConfig", w.BusinessDayConfig)
}

func (s shipService) check(name string) error {
	if err := shipOneText(name+".serviceName", s.ServiceName, maxShipTextRunes); err != nil {
		return err
	}
	if len(s.DeliveryCountries) == 0 || len(s.DeliveryCountries) > maxShipCountries {
		return shipBad(name+".deliveryCountries", "must hold 1 to "+strconv.Itoa(maxShipCountries)+" country codes")
	}
	seen := map[string]bool{}
	for _, c := range s.DeliveryCountries {
		if !shipTerritory.MatchString(c) || seen[c] {
			return shipBad(name+".deliveryCountries", "must be distinct two-letter upper case territory codes")
		}
		seen[c] = true
	}
	if s.Active == nil {
		return shipBad(name+".active", "is required")
	}
	if !shipCurrency.MatchString(s.CurrencyCode) {
		return shipBad(name+".currencyCode", "must be a three-letter upper case currency code")
	}
	if s.ShipmentType != "" && !slices.Contains([]string{"DELIVERY", "LOCAL_DELIVERY", "COLLECTION_POINT"}, s.ShipmentType) {
		return shipBad(name+".shipmentType", "must be DELIVERY, LOCAL_DELIVERY, or COLLECTION_POINT")
	}
	if s.DeliveryTime == nil {
		return shipBad(name+".deliveryTime", "is required")
	}
	if err := s.DeliveryTime.check(name + ".deliveryTime"); err != nil {
		return err
	}
	if s.MinimumOrderValue != nil && s.MinimumOrderValueTable != nil {
		return shipBad(name, "cannot hold both minimumOrderValue and minimumOrderValueTable")
	}
	if s.MinimumOrderValue != nil {
		if err := shipMoney(name+".minimumOrderValue", s.MinimumOrderValue, false, s.CurrencyCode); err != nil {
			return err
		}
	}
	if t := s.MinimumOrderValueTable; t != nil {
		if s.ShipmentType != "COLLECTION_POINT" {
			return shipBad(name+".minimumOrderValueTable", "is for pickup only: shipmentType must be COLLECTION_POINT")
		}
		if err := t.check(name+".minimumOrderValueTable", s.CurrencyCode); err != nil {
			return err
		}
	}
	if c := s.StoreConfig; c != nil {
		if s.ShipmentType != "LOCAL_DELIVERY" {
			return shipBad(name+".storeConfig", "is for local delivery only: shipmentType must be LOCAL_DELIVERY")
		}
		if err := c.check(name + ".storeConfig"); err != nil {
			return err
		}
	}
	if err := s.checkLoyalty(name); err != nil {
		return err
	}
	return s.checkRateGroups(name)
}

func (s shipService) checkLoyalty(name string) error {
	if len(s.LoyaltyPrograms) > maxShipSmall {
		return shipBad(name+".loyaltyPrograms", "holds at most "+strconv.Itoa(maxShipSmall)+" entries")
	}
	for _, p := range s.LoyaltyPrograms {
		if len(p.LoyaltyProgramTiers) > maxShipSmall {
			return shipBad(name+".loyaltyPrograms", "hold at most "+strconv.Itoa(maxShipSmall)+" tiers")
		}
		if err := shipOneText(name+".loyaltyPrograms.programLabel", p.ProgramLabel, maxShipTextRunes); err != nil {
			return err
		}
		for _, t := range p.LoyaltyProgramTiers {
			if err := shipOneText(name+".loyaltyPrograms.tierLabel", t.TierLabel, maxShipTextRunes); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t shipMinimumTable) check(name, currency string) error {
	sets := t.StoreCodeSetWithMovs
	if len(sets) < 2 || len(sets) > maxShipSmall*5 {
		return shipBad(name+".storeCodeSetWithMovs", "needs at least two sets, the last one without storeCodes")
	}
	seen := map[string]bool{}
	for i, set := range sets {
		if err := shipMoney(name+".storeCodeSetWithMovs.value", set.Value, false, currency); err != nil {
			return err
		}
		if (i == len(sets)-1) != (len(set.StoreCodes) == 0) {
			return shipBad(name+".storeCodeSetWithMovs", "must have storeCodes in every set except the last, which has none")
		}
		if len(set.StoreCodes) > maxShipStoreCodes {
			return shipBad(name+".storeCodeSetWithMovs", "hold at most "+strconv.Itoa(maxShipStoreCodes)+" store codes")
		}
		for _, code := range set.StoreCodes {
			if err := shipOneText(name+".storeCodes", code, maxShipTextRunes); err != nil {
				return err
			}
			if seen[code] {
				return shipBad(name+".storeCodes", "must name each store code once")
			}
			seen[code] = true
		}
	}
	return nil
}

func (c shipStoreConfig) check(name string) error {
	if len(c.StoreCodes) > maxShipStoreCodes {
		return shipBad(name+".storeCodes", "holds at most "+strconv.Itoa(maxShipStoreCodes)+" store codes")
	}
	for _, code := range c.StoreCodes {
		if err := shipOneText(name+".storeCodes", code, maxShipTextRunes); err != nil {
			return err
		}
	}
	switch c.StoreServiceType {
	case "ALL_STORES":
	case "SELECTED_STORES":
		if len(c.StoreCodes) == 0 {
			return shipBad(name+".storeCodes", "must name the stores of SELECTED_STORES")
		}
	default:
		return shipBad(name+".storeServiceType", "must be ALL_STORES or SELECTED_STORES")
	}
	if len(c.StoreCodes) == 0 && c.StoreServiceType != "ALL_STORES" {
		return shipBad(name+".storeServiceType", "must be ALL_STORES when storeCodes is empty")
	}
	if r := c.ServiceRadius; r == nil || r.Value == nil || *r.Value < 1 || *r.Value > 100000 ||
		(r.Unit != "MILES" && r.Unit != "KILOMETERS") {
		return shipBad(name+".serviceRadius", "is required for local delivery: value from 1 to 100000 and unit MILES or KILOMETERS")
	}
	if cc := c.CutoffConfig; cc != nil {
		if cc.LocalCutoffTime != nil && cc.StoreCloseOffsetHours != nil {
			return shipBad(name+".cutoffConfig", "cannot hold both localCutoffTime and storeCloseOffsetHours")
		}
		if t := cc.LocalCutoffTime; t != nil {
			if err := shipClock(name+".cutoffConfig.localCutoffTime", t.Hour, t.Minute); err != nil {
				return err
			}
		}
		if h := cc.StoreCloseOffsetHours; h != nil && (*h < 0 || *h > 24) {
			return shipBad(name+".cutoffConfig.storeCloseOffsetHours", "must be from 0 to 24")
		}
	}
	return nil
}

func (d shipDeliveryTime) check(name string) error {
	if len(d.WarehouseBasedDeliveryTimes) > 0 {
		if d.MinTransitDays != nil || d.MaxTransitDays != nil || d.TransitTimeTable != nil {
			return shipBad(name, "cannot hold warehouseBasedDeliveryTimes together with another transit time member")
		}
		if len(d.WarehouseBasedDeliveryTimes) > maxShipSmall {
			return shipBad(name+".warehouseBasedDeliveryTimes", "holds at most "+strconv.Itoa(maxShipSmall)+" entries")
		}
		for _, w := range d.WarehouseBasedDeliveryTimes {
			for field, v := range map[string]string{"carrier": w.Carrier, "carrierService": w.CarrierService,
				"warehouse": w.Warehouse} {
				if err := shipOneText(name+".warehouseBasedDeliveryTimes."+field, v, maxShipTextRunes); err != nil {
					return err
				}
			}
		}
	}
	if d.TransitTimeTable != nil && (d.MinTransitDays != nil || d.MaxTransitDays != nil) {
		return shipBad(name, "cannot hold transitTimeTable together with minTransitDays or maxTransitDays")
	}
	if err := shipPair(name+".transit days", d.MinTransitDays, d.MaxTransitDays, false); err != nil {
		return err
	}
	if err := shipPair(name+".handling days", d.MinHandlingDays, d.MaxHandlingDays, true); err != nil {
		return err
	}
	if c := d.CutoffTime; c != nil {
		if err := shipClock(name+".cutoffTime", c.Hour, c.Minute); err != nil {
			return err
		}
		if err := shipOneText(name+".cutoffTime.timeZone", c.TimeZone, maxShipTextRunes); err != nil {
			return err
		}
	}
	for field, c := range map[string]*shipBusinessDays{"handlingBusinessDayConfig": d.HandlingBusinessDayConfig,
		"transitBusinessDayConfig": d.TransitBusinessDayConfig} {
		if err := checkBusinessDays(name+"."+field, c); err != nil {
			return err
		}
	}
	if t := d.TransitTimeTable; t != nil {
		return t.check(name + ".transitTimeTable")
	}
	return nil
}

// shipPair checks a min and a max in days: max is at least min, and where together is set, both or neither.
func shipPair(name string, lo, hi *number, together bool) error {
	if together && (lo == nil) != (hi == nil) {
		return shipBad(name, "must be set together: pass both the minimum and the maximum or neither")
	}
	if err := shipDays(name, lo, false); err != nil {
		return err
	}
	if err := shipDays(name, hi, false); err != nil {
		return err
	}
	if lo != nil && hi != nil && *hi < *lo {
		return shipBad(name, "must have a maximum that is at least the minimum")
	}
	return nil
}

func (t shipTransitTable) check(name string) error {
	if err := shipTextList(name+".postalCodeGroupNames", t.PostalCodeGroupNames, 0, maxShipLines); err != nil {
		return err
	}
	if err := shipTextList(name+".transitTimeLabels", t.TransitTimeLabels, 0, maxShipLines); err != nil {
		return err
	}
	if len(t.PostalCodeGroupNames) == 0 && len(t.TransitTimeLabels) == 0 {
		return shipBad(name, "needs postalCodeGroupNames, transitTimeLabels, or both")
	}
	if len(t.Rows) == 0 || len(t.Rows) > maxShipLines {
		return shipBad(name+".rows", "must hold 1 to "+strconv.Itoa(maxShipLines)+" rows")
	}
	for _, row := range t.Rows {
		if len(row.Values) == 0 || len(row.Values) > maxShipLines {
			return shipBad(name+".rows", "need 1 to "+strconv.Itoa(maxShipLines)+" values each")
		}
		for _, v := range row.Values {
			if err := shipPair(name+".rows.values", v.MinTransitDays, v.MaxTransitDays, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s shipService) checkRateGroups(name string) error {
	if len(s.RateGroups) > maxShipGroups {
		return shipBad(name+".rateGroups", "holds at most "+strconv.Itoa(maxShipGroups)+" groups")
	}
	labels := map[string]bool{}
	groupNames := map[string]bool{}
	for i, g := range s.RateGroups {
		gn := name + ".rateGroups[" + strconv.Itoa(i) + "]"
		if len(g.ApplicableShippingLabels) == 0 && i != len(s.RateGroups)-1 {
			return shipBad(gn+".applicableShippingLabels", "can be empty only in the last rate group")
		}
		if err := shipTextList(gn+".applicableShippingLabels", g.ApplicableShippingLabels, 0, maxShipLabels); err != nil {
			return err
		}
		for _, l := range g.ApplicableShippingLabels {
			if labels[l] {
				return shipBad(gn+".applicableShippingLabels", "must not overlap those of another rate group")
			}
			labels[l] = true
		}
		if g.Name != "" {
			if err := shipOneText(gn+".name", g.Name, maxShipTextRunes); err != nil {
				return err
			}
			if groupNames[g.Name] {
				return shipBad(gn+".name", "must be unique in the service")
			}
			groupNames[g.Name] = true
		}
		if err := g.check(gn, s.CurrencyCode); err != nil {
			return err
		}
	}
	return nil
}

func (g shipRateGroup) check(name, currency string) error {
	if (g.SingleValue == nil) == (g.MainTable == nil) {
		return shipBad(name, "needs exactly one of singleValue and mainTable")
	}
	if len(g.Subtables) > 0 && g.MainTable == nil {
		return shipBad(name+".subtables", "need a mainTable")
	}
	if len(g.CarrierRates) > maxShipGroups {
		return shipBad(name+".carrierRates", "hold at most "+strconv.Itoa(maxShipGroups)+" entries")
	}
	rates := map[string]bool{}
	for _, r := range g.CarrierRates {
		for field, v := range map[string]string{"name": r.Name, "carrier": r.Carrier, "carrierService": r.CarrierService,
			"originPostalCode": r.OriginPostalCode} {
			if err := shipOneText(name+".carrierRates."+field, v, maxShipTextRunes); err != nil {
				return err
			}
		}
		if rates[r.Name] {
			return shipBad(name+".carrierRates.name", "must be unique in the rate group")
		}
		rates[r.Name] = true
		if r.PercentageAdjustment != "" && !shipDecimalRe.MatchString(r.PercentageAdjustment) {
			return shipBad(name+".carrierRates.percentageAdjustment", "must be a decimal such as 5.4")
		}
		if r.FlatAdjustment != nil {
			if err := shipMoney(name+".carrierRates.flatAdjustment", r.FlatAdjustment, true, currency); err != nil {
				return err
			}
		}
	}
	tables := map[string]bool{}
	if len(g.Subtables) > maxShipGroups {
		return shipBad(name+".subtables", "hold at most "+strconv.Itoa(maxShipGroups)+" tables")
	}
	for _, t := range g.Subtables {
		if err := shipOneText(name+".subtables.name", t.Name, maxShipTextRunes); err != nil {
			return err
		}
		if tables[t.Name] {
			return shipBad(name+".subtables.name", "must be unique in the rate group")
		}
		tables[t.Name] = true
	}
	ref := shipRefs{rates: rates, tables: tables, currency: currency}
	if g.SingleValue != nil {
		return g.SingleValue.check(name+".singleValue", ref, false)
	}
	if err := g.MainTable.check(name+".mainTable", ref); err != nil {
		return err
	}
	for _, t := range g.Subtables {
		if err := t.check(name+".subtables", ref); err != nil {
			return err
		}
	}
	return nil
}

// shipRefs are the names a value may point to inside one rate group, and the currency of the service.
type shipRefs struct {
	rates, tables map[string]bool
	currency      string
}

func (v shipValue) check(name string, ref shipRefs, inTable bool) error {
	set := 0
	if v.NoShipping != nil {
		set++
		if !*v.NoShipping {
			return shipBad(name+".noShipping", "must be true when set")
		}
	}
	if v.FlatRate != nil {
		set++
		if err := shipMoney(name+".flatRate", v.FlatRate, false, ref.currency); err != nil {
			return err
		}
	}
	if v.PricePercentage != "" {
		set++
		if !shipDecimalRe.MatchString(v.PricePercentage) || strings.HasPrefix(v.PricePercentage, "-") {
			return shipBad(name+".pricePercentage", "must be a decimal such as 5.4")
		}
	}
	if v.CarrierRate != "" {
		set++
		if !ref.rates[v.CarrierRate] {
			return shipBad(name+".carrierRate", "must name a carrier rate of the same rate group")
		}
	}
	if v.Subtable != "" {
		set++
		if !inTable || !ref.tables[v.Subtable] {
			return shipBad(name+".subtable", "must name a subtable of the same rate group and is allowed in table cells only")
		}
	}
	if set != 1 {
		return shipBad(name, "needs exactly one of noShipping, flatRate, pricePercentage, carrierRate, and subtable")
	}
	return nil
}

func (t shipTable) check(name string, ref shipRefs) error {
	if t.RowHeaders == nil {
		return shipBad(name+".rowHeaders", "is required")
	}
	rows, err := t.RowHeaders.check(name+".rowHeaders", ref.currency)
	if err != nil {
		return err
	}
	cols := 1
	if t.ColumnHeaders != nil {
		if cols, err = t.ColumnHeaders.check(name+".columnHeaders", ref.currency); err != nil {
			return err
		}
	}
	if len(t.Rows) != rows {
		return shipBad(name+".rows", "must hold one row for every row header")
	}
	for _, row := range t.Rows {
		if len(row.Cells) != cols {
			return shipBad(name+".rows", "must hold one cell for every column header, or one cell without column headers")
		}
		for _, cell := range row.Cells {
			if err := cell.check(name+".rows.cells", ref, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// check validates the one list of the headers and returns its length.
func (h shipHeaders) check(name, currency string) (int, error) {
	kinds := 0
	size := 0
	for _, count := range []int{len(h.Prices), len(h.Weights), len(h.NumberOfItems), len(h.PostalCodeGroupNames),
		len(h.Locations)} {
		if count > 0 {
			kinds++
			size = count
		}
	}
	if kinds != 1 || size > maxShipLines {
		return 0, shipBad(name, "needs exactly one non-empty list of prices, weights, numberOfItems, "+
			"postalCodeGroupNames, and locations, with at most "+strconv.Itoa(maxShipLines)+" entries")
	}
	last := func(i int) bool { return i == size-1 }
	for i, p := range h.Prices {
		if err := shipMoney(name+".prices", &p, true, currency); err != nil {
			return 0, err
		}
		if err := shipBound(name+".prices", p.AmountMicros, last(i)); err != nil {
			return 0, err
		}
	}
	units := map[string]bool{}
	for i, w := range h.Weights {
		if !shipMicros.MatchString(w.AmountMicros) || (w.Unit != "POUND" && w.Unit != "KILOGRAM") {
			return 0, shipBad(name+".weights", "need amountMicros as digits and unit POUND or KILOGRAM")
		}
		units[w.Unit] = true
		if err := shipBound(name+".weights", w.AmountMicros, last(i)); err != nil {
			return 0, err
		}
	}
	if len(units) > 1 {
		return 0, shipBad(name+".weights", "must use one unit")
	}
	for i, n := range h.NumberOfItems {
		if !shipDigits.MatchString(n) && !(n == "infinity" && last(i)) {
			return 0, shipBad(name+".numberOfItems", "must be counts as digits, with infinity only last")
		}
	}
	if err := shipTextList(name+".postalCodeGroupNames", h.PostalCodeGroupNames, 0, maxShipLines); err != nil {
		return 0, err
	}
	for _, l := range h.Locations {
		if err := shipTextList(name+".locations.locationIds", l.LocationIDs, 1, maxShipLines); err != nil {
			return 0, err
		}
	}
	return size, nil
}

// shipBound checks an upper bound of a header: positive, or -1 for infinity on the last entry only.
func shipBound(name, micros string, last bool) error {
	if micros == "-1" {
		if !last {
			return shipBad(name, "can use -1 (infinity) only in the last entry")
		}
		return nil
	}
	if micros == "0" || strings.HasPrefix(micros, "-") {
		return shipBad(name, "must be positive, or -1 in the last entry")
	}
	return nil
}
