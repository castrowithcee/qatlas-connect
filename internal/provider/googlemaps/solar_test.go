package googlemaps

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// solarRewrite sends the Solar host to the test server and refuses every other host, so an address from an
// answer that the tool fetched would fail here and show up as a second request or an error.
type solarRewrite struct {
	target *url.URL
	base   http.RoundTripper
}

func (r solarRewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != "solar.googleapis.com" || req.URL.Scheme != "https" {
		return nil, errors.New("unexpected host")
	}
	clone := req.Clone(req.Context())
	clone.Header.Set(originalHost, req.URL.Host)
	clone.URL.Scheme, clone.URL.Host, clone.Host = r.target.Scheme, r.target.Host, ""
	return r.base.RoundTrip(clone)
}

func newSolarEnvironment(t *testing.T, f *fakeGoogle) *environment {
	t.Helper()
	server := httptest.NewTLSServer(f)
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	previous := transport
	transport = solarRewrite{target: target, base: server.Client().Transport}
	t.Cleanup(func() { transport = previous })
	t.Cleanup(limiters.Replace(keyValue, ratelimit.New(0, time.Now, func(context.Context, time.Duration) error { return nil })))
	reads := 0
	red := &redact.Redactor{}
	lookup := func(name string) string {
		reads++
		if name == keyEnv {
			return keyValue
		}
		return ""
	}
	return &environment{fake: f, core: application.New(registry(t), coreConfig(), secret.NewWith(lookup, nil, nil, red), red),
		red: red, reads: &reads, t: t}
}

const solarPoint = `{"latitude":37.445,"longitude":-122.139}`

// solarMinimalArguments are the smallest valid arguments of each tool.
var solarMinimalArguments = map[string]string{
	solarBuildingInsightsID: `{"location":` + solarPoint + `}`,
	solarDataLayersID:       `{"location":` + solarPoint + `,"radius_meters":10}`,
}

const solarBuilding = `{"name":"buildings/ChIJabc","center":{"latitude":37.445,"longitude":-122.139},` +
	`"boundingBox":{"sw":{"latitude":37.4,"longitude":-122.2},"ne":{"latitude":37.5,"longitude":-122.1}},` +
	`"imageryDate":{"year":2022,"month":5,"day":3},"imageryProcessedDate":{"year":2023,"month":1,"day":2},` +
	`"postalCode":"94040","administrativeArea":"CA","statisticalArea":"tract-1","regionCode":"US","imageryQuality":"HIGH",` +
	`"solarPotential":{"maxArrayPanelsCount":2,"maxArrayAreaMeters2":3.5,"maxSunshineHoursPerYear":1700,` +
	`"carbonOffsetFactorKgPerMwh":400,"panelCapacityWatts":400,"panelHeightMeters":1.7,"panelWidthMeters":1,"panelLifetimeYears":20,` +
	`"wholeRoofStats":{"areaMeters2":100,"groundAreaMeters2":90,"sunshineQuantiles":[1,2,3],"surprise":1},` +
	`"buildingStats":{"areaMeters2":110},` +
	`"roofSegmentStats":[{"pitchDegrees":20,"azimuthDegrees":180,"stats":{"areaMeters2":50},"center":{"latitude":1,"longitude":2},` +
	`"boundingBox":{"sw":{"latitude":1,"longitude":2},"ne":{"latitude":3,"longitude":4}},"planeHeightAtCenterMeters":12.5,` +
	`"polygonGeojson":{"type":"Polygon","coordinates":[[[-1,-1,0],[-1,0,0],[0,0,0],[-1,-1,0]]],"extra":1}}],` +
	`"solarPanels":[{"center":{"latitude":1,"longitude":2},"orientation":"LANDSCAPE","segmentIndex":0,"yearlyEnergyDcKwh":300}],` +
	`"solarPanelConfigs":[{"panelsCount":1,"yearlyEnergyDcKwh":300,"roofSegmentSummaries":[{"pitchDegrees":20,"azimuthDegrees":180,` +
	`"panelsCount":1,"yearlyEnergyDcKwh":300,"segmentIndex":0}]}],` +
	`"financialAnalyses":[{"monthlyBill":{"currencyCode":"USD","units":"100"},"defaultBill":true,"averageKwhPerMonth":600,` +
	`"panelConfigIndex":0,"financialDetails":{"initialAcKwhPerYear":500,"federalIncentive":{"units":"1000","currencyCode":"USD"},` +
	`"netMeteringAllowed":true,"solarPercentage":80,"percentageExportedToGrid":20},` +
	`"leasingSavings":{"leasesAllowed":true,"leasesSupported":false,"annualLeasingCost":{"units":"5"},"savings":{"financiallyViable":true}},` +
	`"cashPurchaseSavings":{"outOfPocketCost":{"units":"9000"},"upfrontCost":{"units":"8000"},"rebateValue":{"units":"1000"},` +
	`"paybackYears":7.5,"savings":{"savingsYear1":{"units":"700","nanos":5},"savingsYear20":{"units":"1"},` +
	`"presentValueOfSavingsYear20":{"units":"2"},"savingsLifetime":{"units":"3"},"presentValueOfSavingsLifetime":{"units":"4"}}},` +
	`"financedPurchaseSavings":{"annualLoanPayment":{"units":"600"},"rebateValue":{"units":"1000"},"loanInterestRate":0.05,` +
	`"savings":{"financiallyViable":false}}}],` +
	`"obstacles":[{"polygonGeojson":{"type":"Polygon","coordinates":[[[1,2,3]]]}}],"deprecatedThing":1},` +
	`"detectedArrays":{"detectionStatus":"DETECTION_STATUS_ARRAYS_DETECTED","latestCaptureDate":{"year":2024,"month":1,"day":1},` +
	`"arrays":[{"polygonGeojson":{"type":"Polygon","coordinates":[[[1,2],[3,4]]]}}]},"extra":"x"}`

func TestSolarBuildingInsightsSendsQueryAndKeepsMembers(t *testing.T) {
	env := newSolarEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", solarBuilding)})
	result, err := env.invokeTool(solarBuildingInsightsID, "read", `{"location":`+solarPoint+`,"required_quality":"MEDIUM",`+
		`"exact_quality_required":true,"experiments":["EXPANDED_COVERAGE","ROOF_GEOMETRY_INSIGHTS"],`+
		`"additional_insights":["ROOF_GEOMETRY","DETECTED_ARRAYS"]}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	r := env.fake.recorded()[0]
	q, _ := url.ParseQuery(r.rawQuery)
	if r.method != http.MethodGet || r.path != "/v1/buildingInsights:findClosest" ||
		r.header.Get(originalHost) != "solar.googleapis.com" || r.header.Get("X-Goog-Api-Key") != keyValue ||
		r.header.Get("X-Goog-FieldMask") != "" || r.body != "" ||
		q.Get("location.latitude") != "37.445" || q.Get("location.longitude") != "-122.139" ||
		q.Get("requiredQuality") != "MEDIUM" || q.Get("exactQualityRequired") != "true" ||
		strings.Join(q["experiments"], ",") != "EXPANDED_COVERAGE,ROOF_GEOMETRY_INSIGHTS" ||
		strings.Join(q["additionalInsights"], ",") != "ROOF_GEOMETRY,DETECTED_ARRAYS" || len(q) != 6 {
		t.Errorf("request = %+v", r)
	}
	for _, text := range []string{r.path, r.rawQuery, result} {
		if strings.Contains(text, keyValue) {
			t.Error("the key reached the URL or the result")
		}
	}
	var got map[string]any
	_ = json.Unmarshal([]byte(result), &got)
	potential := got["solarPotential"].(map[string]any)
	analysis := potential["financialAnalyses"].([]any)[0].(map[string]any)
	cash := analysis["cashPurchaseSavings"].(map[string]any)
	segment := potential["roofSegmentStats"].([]any)[0].(map[string]any)
	polygon := segment["polygonGeojson"].(map[string]any)
	if got["name"] != "buildings/ChIJabc" || got["extra"] != nil || got["postalCode"] != "94040" ||
		got["regionCode"] != "US" || got["imageryQuality"] != "HIGH" ||
		got["imageryDate"].(map[string]any)["year"] != float64(2022) ||
		got["boundingBox"].(map[string]any)["ne"].(map[string]any)["latitude"] != 37.5 ||
		potential["deprecatedThing"] != nil || potential["maxArrayPanelsCount"] != float64(2) ||
		potential["wholeRoofStats"].(map[string]any)["surprise"] != nil ||
		len(potential["wholeRoofStats"].(map[string]any)["sunshineQuantiles"].([]any)) != 3 ||
		potential["panelLifetimeYears"] != float64(20) ||
		potential["solarPanels"].([]any)[0].(map[string]any)["orientation"] != "LANDSCAPE" ||
		len(potential["solarPanelConfigs"].([]any)[0].(map[string]any)["roofSegmentSummaries"].([]any)) != 1 ||
		analysis["defaultBill"] != true || analysis["monthlyBill"].(map[string]any)["units"] != "100" ||
		analysis["financialDetails"].(map[string]any)["netMeteringAllowed"] != true ||
		analysis["leasingSavings"].(map[string]any)["leasesSupported"] != false ||
		cash["paybackYears"] != 7.5 || cash["savings"].(map[string]any)["savingsYear1"].(map[string]any)["nanos"] != float64(5) ||
		analysis["financedPurchaseSavings"].(map[string]any)["loanInterestRate"] != 0.05 ||
		segment["planeHeightAtCenterMeters"] != 12.5 || polygon["type"] != "Polygon" || polygon["extra"] != nil ||
		len(potential["obstacles"].([]any)) != 1 ||
		got["detectedArrays"].(map[string]any)["detectionStatus"] != "DETECTION_STATUS_ARRAYS_DETECTED" ||
		len(got["detectedArrays"].(map[string]any)["arrays"].([]any)) != 1 {
		t.Errorf("result = %s", result)
	}
}

func TestSolarBuildingInsightsQualityLevels(t *testing.T) {
	for _, quality := range solarQualities {
		env := newSolarEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", `{"name":"buildings/x"}`)})
		if _, err := env.invokeTool(solarBuildingInsightsID, "read", `{"location":`+solarPoint+`,"required_quality":"`+quality+`"}`); err != nil {
			t.Errorf("%s: %v", quality, err)
		}
		q, _ := url.ParseQuery(env.fake.recorded()[0].rawQuery)
		if q.Get("requiredQuality") != quality || q.Has("exactQualityRequired") || q.Has("experiments") || q.Has("additionalInsights") {
			t.Errorf("%s: query = %v", quality, q)
		}
	}
}

func TestSolarNoCoverageIsNotFound(t *testing.T) {
	for tool, arguments := range solarMinimalArguments {
		env := newSolarEnvironment(t, &fakeGoogle{answer: reply(404, "application/json", errorObject(404, "NOT_FOUND", "X"))})
		_, err := env.invokeTool(tool, "read", arguments)
		var failed *provider.Error
		if classOf(err) != provider.ClassNotFound || !errors.As(err, &failed) ||
			!strings.Contains(failed.Message, "no solar data") || !strings.Contains(failed.Message, "coverage") ||
			strings.Contains(failed.Message, detailCanary) {
			t.Errorf("%s: err = %v", tool, err)
		}
	}
}

const solarLayers = `{"imageryDate":{"year":2022,"month":5,"day":3},"imageryProcessedDate":{"year":2023,"month":1,"day":2},` +
	`"imageryQuality":"HIGH","dsmUrl":"https://solar.googleapis.com/v1/solar/geoTiff:get?id=dsm",` +
	`"rgbUrl":"https://solar.googleapis.com/v1/solar/geoTiff:get?id=rgb&key=SECRET-ONE",` +
	`"maskUrl":"https://solar.googleapis.com/v1/solar/geoTiff:get?KEY=SECRET-TWO&id=mask#frag",` +
	`"annualFluxUrl":"https://evil.example/v1/solar/geoTiff:get?id=flux",` +
	`"monthlyFluxUrl":"https://solar.googleapis.com/v1/solar/geoTiff:get?k%65y=SECRET-THREE&id=m&api_key=SECRET-FOUR&key=SECRET-FIVE",` +
	`"hourlyShadeUrls":["https://solar.googleapis.com/v1/solar/geoTiff:get?id=h1","http://solar.googleapis.com/v1/x",` +
	`"https://solar.googleapis.com/v1/solar/geoTiff:get?id=h3&key=SECRET-SIX",7],"surprise":1}`

func TestSolarDataLayersRadiusViewAndAddresses(t *testing.T) {
	env := newSolarEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", solarLayers)})
	result, err := env.invokeTool(solarDataLayersID, "read", `{"location":`+solarPoint+`,"radius_meters":175,`+
		`"view":"IMAGERY_AND_ANNUAL_FLUX_LAYERS","required_quality":"LOW","exact_quality_required":false,`+
		`"pixel_size_meters":0.25,"experiments":["EXPANDED_COVERAGE"]}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	// The addresses of the answer are never fetched: Google's host sees exactly the one request.
	if n := len(env.fake.recorded()); n != 1 {
		t.Fatalf("%d requests, want one", n)
	}
	r := env.fake.recorded()[0]
	q, _ := url.ParseQuery(r.rawQuery)
	if r.method != http.MethodGet || r.path != "/v1/dataLayers:get" || r.header.Get(originalHost) != "solar.googleapis.com" ||
		r.header.Get("X-Goog-Api-Key") != keyValue || q.Get("radiusMeters") != "175" ||
		q.Get("view") != "IMAGERY_AND_ANNUAL_FLUX_LAYERS" || q.Get("requiredQuality") != "LOW" ||
		q.Get("exactQualityRequired") != "false" || q.Get("pixelSizeMeters") != "0.25" ||
		q.Get("experiments") != "EXPANDED_COVERAGE" || q.Get("location.latitude") != "37.445" || len(q) != 8 {
		t.Errorf("request = %+v", r)
	}
	for _, secretText := range []string{keyValue, "SECRET", "evil.example", "KEY=", "key=", "frag", "api_key"} {
		if strings.Contains(result, secretText) {
			t.Errorf("result shows %q: %s", secretText, result)
		}
	}
	var got map[string]any
	_ = json.Unmarshal([]byte(result), &got)
	shade, _ := got["hourlyShadeUrls"].([]any)
	if got["dsmUrl"] != "https://solar.googleapis.com/v1/solar/geoTiff:get?id=dsm" ||
		got["rgbUrl"] != "https://solar.googleapis.com/v1/solar/geoTiff:get?id=rgb" ||
		got["maskUrl"] != "https://solar.googleapis.com/v1/solar/geoTiff:get?id=mask" ||
		got["annualFluxUrl"] != nil ||
		got["monthlyFluxUrl"] != "https://solar.googleapis.com/v1/solar/geoTiff:get?id=m" ||
		got["imageryQuality"] != "HIGH" || got["surprise"] != nil ||
		got["imageryDate"].(map[string]any)["day"] != float64(3) ||
		len(shade) != 4 || shade[0] != "https://solar.googleapis.com/v1/solar/geoTiff:get?id=h1" || shade[1] != "" ||
		shade[2] != "https://solar.googleapis.com/v1/solar/geoTiff:get?id=h3" || shade[3] != "" {
		t.Errorf("result = %s", result)
	}
}

func TestSolarLinkRule(t *testing.T) {
	const base = "https://solar.googleapis.com/v1/solar/geoTiff:get"
	for in, want := range map[string]string{
		base + "?id=a":                                          base + "?id=a",
		base + "?id=a&key=K":                                    base + "?id=a",
		base + "?key=K":                                         base,
		base + "?Key=K&KEY=L&id=a":                              base + "?id=a",
		base + "?id=a;key=K":                                    "",
		base + "?id=a;b=c":                                      "",
		base + "?id=a&&b=c":                                     base + "?id=a&b=c",
		base + "?id=a&api-key=K&apiKey=L&API_KEY=M":             base + "?id=a",
		base + "?id=a&key[]=K":                                  base + "?id=a",
		base + "?id=a&k%45y=K":                                  base + "?id=a",
		base + "?id=a&key%3DK":                                  base + "?id=a",
		base + "?id=a&keyring=x":                                base + "?id=a&keyring=x",
		base + "?id=a#key=K":                                    base + "?id=a",
		base + "?id=a&%zz=1":                                    "",
		"http://solar.googleapis.com/v1/x":                      "",
		"https://solar.googleapis.com.evil.example/x":           "",
		"https://solar.googleapis.com" + "@" + "evil.example/x": "",
		"https://solar.googleapis.com\\@evil.example":           "",
		"https://evil.example/https://solar.googleapis.com/":    "",
		"https://solar.googleapis.com:8443/v1/x":                "",
		"https://solar.googleapis.com/v1/x y":                   "",
		"https://solar.googleapis.com":                          "",
		"javascript:alert(1)":                                   "",
		"":                                                      "",
	} {
		if got := solarLink(in); got != want {
			t.Errorf("solarLink(%q) = %q, want %q", in, got, want)
		}
	}
	if got := solarLink(base + "?id=" + strings.Repeat("a", maxAddressBytes)); got != "" {
		t.Error("an over-long address was shown")
	}
}

func TestSolarDataLayersBounds(t *testing.T) {
	ok := map[string]string{
		"zero":         `{"radius_meters":0}`,
		"base":         `{"radius_meters":100}`,
		"250 at 0.25":  `{"radius_meters":250,"pixel_size_meters":0.25,"view":"DSM_LAYER"}`,
		"175 full":     `{"radius_meters":175,"pixel_size_meters":0.25}`,
		"1000 at 1":    `{"radius_meters":1000,"pixel_size_meters":1,"view":"IMAGERY_LAYERS"}`,
		"500 at 0.5":   `{"radius_meters":500,"pixel_size_meters":0.5,"view":"IMAGERY_AND_ANNUAL_FLUX_LAYERS"}`,
		"fraction":     `{"radius_meters":12.5}`,
		"view full":    `{"radius_meters":100,"view":"FULL_LAYERS"}`,
		"view all":     `{"radius_meters":100,"view":"IMAGERY_AND_ALL_FLUX_LAYERS"}`,
		"pixel 0.1":    `{"radius_meters":100,"pixel_size_meters":0.1}`,
		"pixel 0.5":    `{"radius_meters":100,"pixel_size_meters":0.5}`,
		"quality BASE": `{"radius_meters":100,"required_quality":"BASE"}`,
	}
	for name, extra := range ok {
		env := newSolarEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", `{"imageryQuality":"HIGH"}`)})
		if _, err := env.invokeTool(solarDataLayersID, "read", `{"location":`+solarPoint+`,`+extra[1:]); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	refused := map[string]string{
		"no radius":            `{"location":` + solarPoint + `}`,
		"negative radius":      `{"location":` + solarPoint + `,"radius_meters":-1}`,
		"radius over 1000":     `{"location":` + solarPoint + `,"radius_meters":1001,"pixel_size_meters":1,"view":"DSM_LAYER"}`,
		"101 at default":       `{"location":` + solarPoint + `,"radius_meters":101,"view":"DSM_LAYER"}`,
		"251 at 0.25":          `{"location":` + solarPoint + `,"radius_meters":251,"pixel_size_meters":0.25,"view":"DSM_LAYER"}`,
		"176 without view":     `{"location":` + solarPoint + `,"radius_meters":176,"pixel_size_meters":0.25}`,
		"176 full layers":      `{"location":` + solarPoint + `,"radius_meters":176,"pixel_size_meters":0.25,"view":"FULL_LAYERS"}`,
		"176 all flux":         `{"location":` + solarPoint + `,"radius_meters":176,"pixel_size_meters":0.25,"view":"IMAGERY_AND_ALL_FLUX_LAYERS"}`,
		"bad pixel size":       `{"location":` + solarPoint + `,"radius_meters":10,"pixel_size_meters":0.3}`,
		"pixel zero":           `{"location":` + solarPoint + `,"radius_meters":10,"pixel_size_meters":0}`,
		"bad view":             `{"location":` + solarPoint + `,"radius_meters":10,"view":"ALL"}`,
		"unspecified view":     `{"location":` + solarPoint + `,"radius_meters":10,"view":"DATA_LAYER_VIEW_UNSPECIFIED"}`,
		"bad quality":          `{"location":` + solarPoint + `,"radius_meters":10,"required_quality":"high"}`,
		"unspecified quality":  `{"location":` + solarPoint + `,"radius_meters":10,"required_quality":"IMAGERY_QUALITY_UNSPECIFIED"}`,
		"roof experiment":      `{"location":` + solarPoint + `,"radius_meters":10,"experiments":["ROOF_GEOMETRY_INSIGHTS"]}`,
		"duplicate experiment": `{"location":` + solarPoint + `,"radius_meters":10,"experiments":["EXPANDED_COVERAGE","EXPANDED_COVERAGE"]}`,
		"empty experiments":    `{"location":` + solarPoint + `,"radius_meters":10,"experiments":[]}`,
		"text radius":          `{"location":` + solarPoint + `,"radius_meters":"10"}`,
		"no location":          `{"radius_meters":10}`,
		"latitude over":        `{"location":{"latitude":90.1,"longitude":0},"radius_meters":10}`,
		"longitude under":      `{"location":{"latitude":0,"longitude":-181},"radius_meters":10}`,
		"address":              `{"location":` + solarPoint + `,"radius_meters":10,"address":"Paris"}`,
		"insights":             `{"location":` + solarPoint + `,"radius_meters":10,"additional_insights":["ROOF_GEOMETRY"]}`,
		"key":                  `{"location":` + solarPoint + `,"radius_meters":10,"key":"x"}`,
		"url":                  `{"location":` + solarPoint + `,"radius_meters":10,"url":"https://evil.example"}`,
	}
	for name, arguments := range refused {
		env := newSolarEnvironment(t, &fakeGoogle{})
		if _, err := env.invokeTool(solarDataLayersID, "read", arguments); err == nil {
			t.Errorf("%s: no error", name)
		}
		if len(env.fake.recorded()) != 0 || *env.reads != 0 {
			t.Errorf("%s: %d requests, %d secret reads", name, len(env.fake.recorded()), *env.reads)
		}
	}
	// The handler checks the same rules itself, before any secret access.
	for name, arguments := range map[string]string{
		"radius":  `{"location":` + solarPoint + `,"radius_meters":2000,"pixel_size_meters":1,"view":"DSM_LAYER"}`,
		"view":    `{"location":` + solarPoint + `,"radius_meters":200,"pixel_size_meters":0.25}`,
		"pixel":   `{"location":` + solarPoint + `,"radius_meters":10,"pixel_size_meters":0.3}`,
		"quality": `{"location":` + solarPoint + `,"radius_meters":10,"required_quality":"UNSPECIFIED"}`,
	} {
		var invalid *provider.InvalidRequestError
		_, err := handleSolarDataLayers(context.Background(), &config.Resolved{BaseURL: apiRoot}, nil, nil, json.RawMessage(arguments))
		if !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want an invalid request", name, err)
		}
	}
}

func TestSolarBuildingInsightsRefusesBeforeAnyIO(t *testing.T) {
	for name, arguments := range map[string]string{
		"no location":                 `{}`,
		"roof experiment alone":       `{"location":` + solarPoint + `,"experiments":["ROOF_GEOMETRY_INSIGHTS"]}`,
		"roof experiment with arrays": `{"location":` + solarPoint + `,"experiments":["ROOF_GEOMETRY_INSIGHTS"],"additional_insights":["DETECTED_ARRAYS"]}`,
		"bad insight":                 `{"location":` + solarPoint + `,"additional_insights":["ALL"]}`,
		"unspecified insight":         `{"location":` + solarPoint + `,"additional_insights":["ADDITIONAL_INSIGHTS_UNSPECIFIED"]}`,
		"duplicate insight":           `{"location":` + solarPoint + `,"additional_insights":["ROOF_GEOMETRY","ROOF_GEOMETRY"]}`,
		"bad experiment":              `{"location":` + solarPoint + `,"experiments":["X"]}`,
		"bad quality":                 `{"location":` + solarPoint + `,"required_quality":"BEST"}`,
		"text exact":                  `{"location":` + solarPoint + `,"exact_quality_required":"yes"}`,
		"radius":                      `{"location":` + solarPoint + `,"radius_meters":10}`,
		"view":                        `{"location":` + solarPoint + `,"view":"DSM_LAYER"}`,
		"no longitude":                `{"location":{"latitude":1}}`,
		"latitude under":              `{"location":{"latitude":-91,"longitude":0}}`,
		"text coordinate":             `{"location":{"latitude":"1","longitude":"2"}}`,
		"key":                         `{"location":` + solarPoint + `,"key":"x"}`,
		"target":                      `{"location":` + solarPoint + `,"target":"x"}`,
	} {
		env := newSolarEnvironment(t, &fakeGoogle{})
		if _, err := env.invokeTool(solarBuildingInsightsID, "read", arguments); err == nil {
			t.Errorf("%s: no error", name)
		}
		if len(env.fake.recorded()) != 0 || *env.reads != 0 {
			t.Errorf("%s: %d requests, %d secret reads", name, len(env.fake.recorded()), *env.reads)
		}
	}
	for _, resolved := range []*config.Resolved{{BaseURL: "https://solar.googleapis.com"}, {BaseURL: apiRoot, Target: "x"}} {
		for _, handler := range []func(context.Context, *config.Resolved, *secret.Resolver, *redact.Redactor, json.RawMessage) (any, error){
			handleSolarBuildingInsights, handleSolarDataLayers} {
			if _, err := handler(context.Background(), resolved, nil, nil, json.RawMessage(`{"location":`+solarPoint+`,"radius_meters":10}`)); err == nil {
				t.Errorf("resolved %+v was accepted", resolved)
			}
		}
	}
	var invalid *provider.InvalidRequestError
	_, err := handleSolarBuildingInsights(context.Background(), &config.Resolved{BaseURL: apiRoot}, nil, nil,
		json.RawMessage(`{"location":{"latitude":95,"longitude":0}}`))
	if !errors.As(err, &invalid) {
		t.Errorf("err = %v, want an invalid request", err)
	}
	_, err = handleSolarBuildingInsights(context.Background(), &config.Resolved{BaseURL: apiRoot}, nil, nil,
		json.RawMessage(`{"location":`+solarPoint+`,"experiments":["ROOF_GEOMETRY_INSIGHTS"]}`))
	if !errors.As(err, &invalid) {
		t.Errorf("err = %v, want an invalid request", err)
	}
}

func repeatJSON(item string, n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = item
	}
	return strings.Join(parts, ",")
}

func TestSolarListsAndTextsAreCut(t *testing.T) {
	body := `{"postalCode":"` + strings.Repeat("n", 500) + `","name":"buildings/` + strings.Repeat("a", 257) + `","solarPotential":{` +
		`"solarPanels":[` + repeatJSON(`{"segmentIndex":1}`, 800) + `],` +
		`"solarPanelConfigs":[` + repeatJSON(`{"panelsCount":4,"roofSegmentSummaries":[`+repeatJSON(`{"panelsCount":1}`, 80)+`]}`, 600) + `],` +
		`"roofSegmentStats":[` + repeatJSON(`{"pitchDegrees":1}`, 150) + `],` +
		`"obstacles":[` + repeatJSON(`{"polygonGeojson":{"type":"Polygon"}}`, 300) + `],` +
		`"financialAnalyses":[` + repeatJSON(`{"defaultBill":false}`, 80) + `]},` +
		`"detectedArrays":{"arrays":[` + repeatJSON(`{"polygonGeojson":{"type":"Polygon"}}`, 150) + `]}}`
	if len(body) > maxResponseBytes {
		t.Fatalf("test body of %d bytes exceeds the response limit", len(body))
	}
	env := newSolarEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", body)})
	result, err := env.invokeTool(solarBuildingInsightsID, "read", `{"location":`+solarPoint+`}`)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	var got struct {
		PostalCode     string  `json:"postalCode"`
		Name           *string `json:"name"`
		SolarPotential struct {
			SolarPanels       []any `json:"solarPanels"`
			SolarPanelConfigs []struct {
				RoofSegmentSummaries []any `json:"roofSegmentSummaries"`
			} `json:"solarPanelConfigs"`
			RoofSegmentStats  []any `json:"roofSegmentStats"`
			Obstacles         []any `json:"obstacles"`
			FinancialAnalyses []any `json:"financialAnalyses"`
		} `json:"solarPotential"`
		DetectedArrays struct {
			Arrays []any `json:"arrays"`
		} `json:"detectedArrays"`
	}
	if json.Unmarshal([]byte(result), &got) != nil {
		t.Fatalf("result = %.200s", result)
	}
	sp := got.SolarPotential
	if len([]rune(got.PostalCode)) != maxShortRunes+1 || got.Name != nil || len(sp.SolarPanels) != maxSolarPanels ||
		len(sp.SolarPanelConfigs) != maxSolarConfigs ||
		len(sp.SolarPanelConfigs[0].RoofSegmentSummaries) != maxSolarSegmentSums ||
		len(sp.RoofSegmentStats) != maxSolarSegments || len(sp.Obstacles) != maxSolarObstacles ||
		len(sp.FinancialAnalyses) != maxSolarAnalyses || len(got.DetectedArrays.Arrays) != maxSolarArrays {
		t.Errorf("a list or text was not cut: postal code %d runes, %d panels, %d configs", len([]rune(got.PostalCode)),
			len(sp.SolarPanels), len(sp.SolarPanelConfigs))
	}
}

func TestSolarPolygonsAndQuantilesAreCut(t *testing.T) {
	ring := "[" + repeatJSON("[1,2,3,4]", 300) + "]"
	body := `{"solarPotential":{"roofSegmentStats":[{"polygonGeojson":{"type":"Polygon","coordinates":[` +
		repeatJSON(ring, 15) + `]},"stats":{"sunshineQuantiles":[` + repeatJSON("1", 90) + `]}}]}}`
	env := newSolarEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", body)})
	result, err := env.invokeTool(solarBuildingInsightsID, "read", `{"location":`+solarPoint+`}`)
	var got struct {
		SolarPotential struct {
			RoofSegmentStats []struct {
				Polygon struct {
					Coordinates [][][]float64 `json:"coordinates"`
				} `json:"polygonGeojson"`
				Stats struct {
					Quantiles []float64 `json:"sunshineQuantiles"`
				} `json:"stats"`
			} `json:"roofSegmentStats"`
		} `json:"solarPotential"`
	}
	if err != nil || json.Unmarshal([]byte(result), &got) != nil || len(got.SolarPotential.RoofSegmentStats) != 1 {
		t.Fatalf("result = %.200s, err = %v", result, err)
	}
	segment := got.SolarPotential.RoofSegmentStats[0]
	if len(segment.Stats.Quantiles) != maxSolarQuantiles || len(segment.Polygon.Coordinates) != maxSolarRings ||
		len(segment.Polygon.Coordinates[0]) != maxSolarRingPoints ||
		len(segment.Polygon.Coordinates[0][0]) != maxSolarCoordinates {
		t.Errorf("a polygon or list was not cut: %d loops", len(segment.Polygon.Coordinates))
	}
}

func TestSolarShadeListIsCutToTwelveMonths(t *testing.T) {
	var urls []string
	for i := 0; i < 20; i++ {
		urls = append(urls, `"https://solar.googleapis.com/v1/solar/geoTiff:get?id=h`+itoa(i)+`"`)
	}
	env := newSolarEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", `{"hourlyShadeUrls":[`+strings.Join(urls, ",")+`]}`)})
	result, err := env.invokeTool(solarDataLayersID, "read", `{"location":`+solarPoint+`,"radius_meters":10}`)
	var got struct {
		Shade []string `json:"hourlyShadeUrls"`
	}
	if err != nil || json.Unmarshal([]byte(result), &got) != nil || len(got.Shade) != solarShadeMonths ||
		!strings.HasSuffix(got.Shade[11], "id=h11") {
		t.Errorf("result = %s, err = %v", result, err)
	}
}

func TestSolarErrorFormsKeepProviderTextApart(t *testing.T) {
	for tool, arguments := range solarMinimalArguments {
		for _, tc := range []struct {
			status int
			body   string
			class  provider.Class
			text   string
		}{
			{401, errorObject(401, "UNAUTHENTICATED", "CREDENTIALS_MISSING"), provider.ClassAuth, ""},
			{403, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED"), provider.ClassPermission, "Solar API"},
			{429, errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED"), provider.ClassRateLimited, ""},
			{404, errorObject(404, "NOT_FOUND", "X"), provider.ClassNotFound, "no solar data"},
			{400, errorObject(400, "INVALID_ARGUMENT", "X"), provider.ClassInvalidResponse, "coverage"},
			{500, errorObject(500, "INTERNAL", "X"), provider.ClassProviderError, ""},
		} {
			env := newSolarEnvironment(t, &fakeGoogle{answer: reply(tc.status, "application/json", tc.body)})
			_, err := env.invokeTool(tool, "read", arguments)
			if classOf(err) != tc.class {
				t.Errorf("%s %d: class = %q, err = %v", tool, tc.status, classOf(err), err)
				continue
			}
			var failed *provider.Error
			_ = errors.As(err, &failed)
			if strings.Contains(failed.Message, detailCanary) || strings.Contains(err.Error(), keyValue) ||
				strings.Contains(failed.Message, "Places") || strings.Contains(failed.Message, "Weather") ||
				!strings.Contains(failed.Message, tc.text) {
				t.Errorf("%s %d: message = %q", tool, tc.status, failed.Message)
			}
			if tc.status == 404 || tc.status == 400 || tc.status == 403 {
				if failed.Original == nil {
					t.Errorf("%s %d: the provider text is not kept apart", tool, tc.status)
				}
			}
			if len(env.fake.recorded()) != 1 {
				t.Errorf("%s %d: %d requests, want one", tool, tc.status, len(env.fake.recorded()))
			}
		}
	}
}

func TestSolarUnusableAnswers(t *testing.T) {
	for name, answer := range map[string]func(http.ResponseWriter, *http.Request){
		"not json":     reply(200, "text/plain", "x"),
		"empty":        reply(200, "application/json", `{}`),
		"only unknown": reply(200, "application/json", `{"surprise":1}`),
		"array":        reply(200, "application/json", `[]`),
		"null":         reply(200, "application/json", `null`),
		"redirect":     reply(302, "", ""),
	} {
		env := newSolarEnvironment(t, &fakeGoogle{answer: answer})
		_, err := env.invokeTool(solarBuildingInsightsID, "read", `{"location":`+solarPoint+`}`)
		if classOf(err) != provider.ClassInvalidResponse && classOf(err) != provider.ClassProviderError {
			t.Errorf("%s: err = %v", name, err)
		}
		if n := len(env.fake.recorded()); n != 1 {
			t.Errorf("%s: %d requests", name, n)
		}
	}
	env := newSolarEnvironment(t, &fakeGoogle{})
	transport = failingTransport{err: context.DeadlineExceeded}
	if _, err := env.invokeTool(solarDataLayersID, "read", `{"location":`+solarPoint+`,"radius_meters":10}`); classOf(err) != provider.ClassTimeout &&
		classOf(err) != provider.ClassProviderError {
		t.Errorf("timeout err = %v", err)
	}
}

func TestSolarToolsRiskAndDescription(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, tc := range []struct {
		id       string
		required int
		extra    []string
	}{
		{solarBuildingInsightsID, 1, []string{"building", "ROOF_GEOMETRY", "pre-GA"}},
		{solarDataLayersID, 2, []string{"never fetched", "Enterprise SKU", "radius_meters", "175"}},
	} {
		var d = solarBuildingInsights
		if tc.id == solarDataLayersID {
			d = solarDataLayers
		}
		inProfile := false
		for _, id := range metadata.Profiles[0].Tools {
			inProfile = inProfile || id == tc.id
		}
		if d.ID != tc.id || d.Risk != readRisk || !inProfile {
			t.Errorf("%s: risk = %+v, in profile %v", tc.id, d.Risk, inProfile)
		}
		for _, want := range append([]string{"attribution", "storage rules", "charges above the free usage", "untrusted",
			"google.maps.geocoding.geocode", "Solar API", "does not verify the SKU", "coordinate is sent to Google"}, tc.extra...) {
			if !strings.Contains(d.Description, want) {
				t.Errorf("%s: description lacks %q", tc.id, want)
			}
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		}
		if json.Unmarshal(d.InputSchema, &schema) != nil || len(schema.Required) != tc.required {
			t.Fatalf("%s: input schema = %s", tc.id, d.InputSchema)
		}
		for name := range schema.Properties {
			documented := false
			for _, argument := range d.Arguments {
				documented = documented || argument.Name == name
			}
			if !documented {
				t.Errorf("%s: argument %s is undocumented", tc.id, name)
			}
		}
		for _, argument := range d.Arguments {
			if schema.Properties[argument.Name] == nil {
				t.Errorf("%s: documented argument %s is not in the schema", tc.id, argument.Name)
			}
		}
		for _, banned := range []string{"key", "target", "url", "id"} {
			if schema.Properties[banned] != nil {
				t.Errorf("%s: tool takes %s", tc.id, banned)
			}
		}
		var out struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if json.Unmarshal(d.OutputSchema, &out) != nil {
			t.Fatalf("%s: output schema = %s", tc.id, d.OutputSchema)
		}
		for name := range out.Properties {
			documented := false
			for _, field := range d.Fields {
				documented = documented || field.Name == name
			}
			if !documented {
				t.Errorf("%s: result member %s is undocumented", tc.id, name)
			}
		}
	}
	if solarRoot != "https://solar.googleapis.com" || metadata.ValidateBaseURL(solarRoot) == nil {
		t.Error("the Solar origin is not fixed or may be a base URL")
	}
}
