package googlecloud

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	writeUploadPath = "/upload/storage/v1/b/example-bucket/o"
	otherBucketPath = bucketsPath + "/other-bucket"
	archiveBucket   = bucketsPath + "/archive-bucket"
)

var objectWriteTools = []string{projectsGetID, objectsInsertID, objectsCopyID, objectsComposeID, objectsPatchID}

// writeStorage is an environment bound to the project of the test bucket that offers the four write tools. The
// buckets example-bucket and archive-bucket belong to the project, other-bucket does not.
func writeStorage(t *testing.T) *environment {
	t.Helper()
	e := createEnvironment(t, []string{"projects/" + storageProjectNumber}, objectWriteTools)
	e.fake.routes[bucketRoute] = okReply(bucketBody("example-bucket", storageProjectNumber))
	e.fake.routes[archiveBucket] = okReply(bucketBody("archive-bucket", storageProjectNumber))
	e.fake.routes[otherBucketPath] = okReply(bucketBody("other-bucket", foreignNumber))
	e.fake.routes[writeUploadPath] = okReply(objectBody("notes/readme.txt"))
	return e
}

// posts are the requests that are not a read of a bucket.
func changes(api []seen) []seen {
	var out []seen
	for _, r := range api {
		if r.method != http.MethodGet {
			out = append(out, r)
		}
	}
	return out
}

type uploadParts struct {
	metadata map[string]any
	typ      string
	content  []byte
}

func parseUpload(t *testing.T, r seen) uploadParts {
	t.Helper()
	media, params, err := mime.ParseMediaType(r.header.Get("Content-Type"))
	if err != nil || media != "multipart/related" {
		t.Fatalf("content type = %q", r.header.Get("Content-Type"))
	}
	reader := multipart.NewReader(strings.NewReader(r.body), params["boundary"])
	var out uploadParts
	for i := 0; ; i++ {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(part)
		switch i {
		case 0:
			if err := json.Unmarshal(data, &out.metadata); err != nil {
				t.Fatalf("metadata part %q: %v", data, err)
			}
		case 1:
			out.typ, out.content = part.Header.Get("Content-Type"), data
		default:
			t.Fatalf("part %d", i)
		}
	}
	if out.content == nil && out.metadata == nil {
		t.Fatal("no parts")
	}
	return out
}

func TestObjectsInsertSendsOneMultipartUploadAfterBindingTheBucket(t *testing.T) {
	e := writeStorage(t)
	out, err := e.invokeConfirmed(objectsInsertID, `{"bucket":"example-bucket","name":"notes/readme.txt",`+
		`"content":"hello ä","content_type":"text/markdown","cache_control":"no-cache","metadata":{"owner":"team"},`+
		`"storage_class":"NEARLINE","custom_time":"2026-12-31T00:00:00Z","predefined_acl":"private",`+
		`"kms_key_name":"projects/p/locations/eu/keyRings/r/cryptoKeys/k","user_project":"`+storageProjectNumber+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 2 || api[0].method != http.MethodGet || !strings.Contains(api[0].url, bucketRoute) ||
		api[1].method != http.MethodPost {
		t.Fatalf("requests = %+v", api)
	}
	u, _ := url.Parse(api[1].url)
	if u.Host != "storage.googleapis.com" || u.Path != writeUploadPath {
		t.Errorf("url = %s", api[1].url)
	}
	want := url.Values{"uploadType": {"multipart"}, "ifGenerationMatch": {"0"}, "predefinedAcl": {"private"},
		"kmsKeyName": {"projects/p/locations/eu/keyRings/r/cryptoKeys/k"}, "userProject": {storageProjectNumber}}
	if got := u.Query(); len(got) != len(want) {
		t.Errorf("query = %v", got)
	} else {
		for name, values := range want {
			if !slices.Equal(got[name], values) {
				t.Errorf("query %s = %v", name, got[name])
			}
		}
	}
	parts := parseUpload(t, api[1])
	if string(parts.content) != "hello ä" || parts.typ != "text/markdown" || parts.metadata["name"] != "notes/readme.txt" ||
		parts.metadata["contentType"] != "text/markdown" || parts.metadata["storageClass"] != "NEARLINE" ||
		parts.metadata["cacheControl"] != "no-cache" || parts.metadata["customTime"] != "2026-12-31T00:00:00Z" ||
		parts.metadata["metadata"].(map[string]any)["owner"] != "team" || len(parts.metadata) != 6 {
		t.Errorf("parts = %+v", parts)
	}
	if _, has := parts.metadata["acl"]; has {
		t.Errorf("metadata carries an acl: %v", parts.metadata)
	}
	var row map[string]any
	if json.Unmarshal([]byte(out), &row) != nil || row["name"] != "notes/readme.txt" || row["bucket"] != "example-bucket" {
		t.Errorf("result = %s", out)
	}
	if strings.Contains(out, "hello") {
		t.Errorf("result holds the content: %s", out)
	}
}

func TestObjectsInsertDecodesBase64AndKeepsBinaryBytes(t *testing.T) {
	e := writeStorage(t)
	data := []byte{0, 1, 2, 0xff, 0xfe, '\n'}
	if _, err := e.invokeConfirmed(objectsInsertID, `{"bucket":"example-bucket","name":"notes/readme.txt",`+
		`"content":"`+base64.StdEncoding.EncodeToString(data)+`","content_encoding_kind":"base64"}`); err != nil {
		t.Fatal(err)
	}
	post := changes(e.fake.api())
	if len(post) != 1 {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	parts := parseUpload(t, post[0])
	if string(parts.content) != string(data) || parts.typ != "application/octet-stream" {
		t.Errorf("content = %v type %q", parts.content, parts.typ)
	}
	if _, has := parts.metadata["contentType"]; has {
		t.Errorf("metadata = %v", parts.metadata)
	}
}

func TestObjectsInsertRefusesToOverwriteUnlessAskedAndSaysSo(t *testing.T) {
	const args = `{"bucket":"example-bucket","name":"notes/readme.txt","content":"secret-body-canary"`
	e := writeStorage(t)
	e.fake.routes[writeUploadPath] = reply{status: 412, body: errorObject(412, "FAILED_PRECONDITION", "")}
	_, err := e.invokeConfirmed(objectsInsertID, args+`}`)
	if err == nil || classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "precondition") ||
		strings.Contains(err.Error(), "secret-body-canary") || strings.Contains(err.Error(), "notes/readme.txt") {
		t.Fatalf("err = %v (%s)", err, classOf(err))
	}
	post := changes(e.fake.api())
	if len(post) != 1 || !strings.Contains(post[0].url, "ifGenerationMatch=0") {
		t.Errorf("requests = %+v", post)
	}
	// With overwrite the caller's precondition decides, and none is sent when none is given.
	for name, c := range map[string]struct{ tail, want string }{
		"none": {`,"overwrite":true}`, ""}, "given": {`,"overwrite":true,"if_generation_match":"7"}`, "7"}} {
		e := writeStorage(t)
		if _, err := e.invokeConfirmed(objectsInsertID, args+c.tail); err != nil {
			t.Fatal(err)
		}
		post := changes(e.fake.api())
		u, _ := url.Parse(post[0].url)
		if got := u.Query().Get("ifGenerationMatch"); len(post) != 1 || got != c.want {
			t.Errorf("%s: ifGenerationMatch = %q, requests %+v", name, got, post)
		}
	}
	// A precondition of the caller without overwrite would hide the protection, so it is refused before I/O.
	e = writeStorage(t)
	if _, err := e.invokeConfirmed(objectsInsertID, args+`,"if_generation_match":"7"}`); !isInvalidRequest(err) {
		t.Errorf("if_generation_match 7 without overwrite: %v", err)
	}
	noIO(t, e, "if_generation_match without overwrite")
	e = writeStorage(t)
	if _, err := e.invokeConfirmed(objectsInsertID, args+`,"if_generation_match":"0"}`); err != nil {
		t.Errorf("if_generation_match 0: %v", err)
	}
}

func TestObjectsWritesRefuseEveryPublicACLBeforeAnyIO(t *testing.T) {
	cases := map[string][2]string{
		"insert publicRead":              {objectsInsertID, `{"bucket":"example-bucket","name":"a","content":"x","predefined_acl":"publicRead"}`},
		"insert authenticatedRead":       {objectsInsertID, `{"bucket":"example-bucket","name":"a","content":"x","predefined_acl":"authenticatedRead"}`},
		"insert acl object":              {objectsInsertID, `{"bucket":"example-bucket","name":"a","content":"x","acl":[{"entity":"allUsers","role":"READER"}]}`},
		"copy publicRead":                {objectsCopyID, `{"source_bucket":"example-bucket","source_object":"a","destination_bucket":"archive-bucket","destination_object":"b","destination_predefined_acl":"publicRead"}`},
		"copy authenticatedRead":         {objectsCopyID, `{"source_bucket":"example-bucket","source_object":"a","destination_bucket":"archive-bucket","destination_object":"b","destination_predefined_acl":"authenticatedRead"}`},
		"copy acl in metadata":           {objectsCopyID, `{"source_bucket":"example-bucket","source_object":"a","destination_bucket":"archive-bucket","destination_object":"b","destination_metadata":{"acl":[{"entity":"allUsers","role":"READER"}]}}`},
		"compose publicRead":             {objectsComposeID, `{"bucket":"example-bucket","destination_object":"c","source_objects":[{"name":"a"},{"name":"b"}],"destination_predefined_acl":"publicRead"}`},
		"patch publicRead":               {objectsPatchID, `{"bucket":"example-bucket","object":"a","predefined_acl":"publicRead"}`},
		"patch authenticatedRead":        {objectsPatchID, `{"bucket":"example-bucket","object":"a","predefined_acl":"authenticatedRead"}`},
		"patch acl object":               {objectsPatchID, `{"bucket":"example-bucket","object":"a","acl":[{"entity":"allUsers","role":"READER"}]}`},
		"insert customer key":            {objectsInsertID, `{"bucket":"example-bucket","name":"a","content":"x","x-goog-encryption-key":"k"}`},
		"copy customer key":              {objectsCopyID, `{"source_bucket":"example-bucket","source_object":"a","destination_bucket":"archive-bucket","destination_object":"b","x-goog-copy-source-encryption-key":"k"}`},
		"insert relative bucket URL":     {objectsInsertID, `{"bucket":"https://evil.example/x","name":"a","content":"x"}`},
		"insert content not encoded":     {objectsInsertID, `{"bucket":"example-bucket","name":"a","content":"x","content_encoding_kind":"hex"}`},
		"insert unknown storage class":   {objectsInsertID, `{"bucket":"example-bucket","name":"a","content":"x","storage_class":"COLD"}`},
		"insert bad custom time":         {objectsInsertID, `{"bucket":"example-bucket","name":"a","content":"x","custom_time":"yesterday, 5 o'clock"}`},
		"insert bad kms key":             {objectsInsertID, `{"bucket":"example-bucket","name":"a","content":"x","kms_key_name":"my-key"}`},
		"insert name dot dot":            {objectsInsertID, `{"bucket":"example-bucket","name":"..","content":"x"}`},
		"insert name control":            {objectsInsertID, `{"bucket":"example-bucket","name":"a\nb","content":"x"}`},
		"insert name bidi":               {objectsInsertID, `{"bucket":"example-bucket","name":"a‮b","content":"x"}`},
		"patch nothing":                  {objectsPatchID, `{"bucket":"example-bucket","object":"a"}`},
		"patch storage class":            {objectsPatchID, `{"bucket":"example-bucket","object":"a","storage_class":"COLDLINE"}`},
		"patch key set and removed":      {objectsPatchID, `{"bucket":"example-bucket","object":"a","metadata":{"k":"v"},"remove_metadata_keys":["k"]}`},
		"patch metadata value too large": {objectsPatchID, `{"bucket":"example-bucket","object":"a","metadata":{"k":"` + strings.Repeat("v", 1025) + `"}}`},
	}
	for name, c := range cases {
		e := writeStorage(t)
		if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
			t.Errorf("%s: accepted", name)
		}
		noIO(t, e, name)
	}
}

func TestObjectsWriteHandlersRefuseAPublicACLWithoutTheSchema(t *testing.T) {
	// The schema refuses these first; the handler checks again.
	e := writeStorage(t)
	call := func(h func() (any, error), name string) {
		if _, err := h(); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	r, s, red := e.resolved(), e.secrets(), e.red
	ctx := context.Background()
	call(func() (any, error) {
		return handleObjectsInsert(ctx, r, s, red, json.RawMessage(
			`{"bucket":"example-bucket","name":"a","content":"x","predefined_acl":"publicRead"}`))
	}, "insert")
	call(func() (any, error) {
		return handleObjectsCopy(ctx, r, s, red, json.RawMessage(`{"source_bucket":"example-bucket",`+
			`"source_object":"a","destination_bucket":"example-bucket","destination_object":"b",`+
			`"destination_predefined_acl":"authenticatedRead"}`))
	}, "copy")
	call(func() (any, error) {
		return handleObjectsCompose(ctx, r, s, red, json.RawMessage(`{"bucket":"example-bucket","destination_object":"c",`+
			`"source_objects":[{"name":"a"},{"name":"b"}],"destination_predefined_acl":"publicRead"}`))
	}, "compose")
	call(func() (any, error) {
		return handleObjectsPatch(ctx, r, s, red, json.RawMessage(
			`{"bucket":"example-bucket","object":"a","predefined_acl":"publicRead"}`))
	}, "patch")
	noIO(t, e, "handlers")
}

func TestObjectsInsertRefusesContentOverTheLimitAndBadBase64BeforeIO(t *testing.T) {
	over := strings.Repeat("a", maxUploadBytes+1)
	e := writeStorage(t)
	r, s, red := e.resolved(), e.secrets(), e.red
	for name, raw := range map[string]string{
		"text over the limit": `{"bucket":"example-bucket","name":"a","content":"` + over + `"}`,
		"base64 over the limit": `{"bucket":"example-bucket","name":"a","content_encoding_kind":"base64","content":"` +
			base64.StdEncoding.EncodeToString([]byte(over)) + `"}`,
		"bad base64":     `{"bucket":"example-bucket","name":"a","content_encoding_kind":"base64","content":"!!!"}`,
		"unpadded":       `{"bucket":"example-bucket","name":"a","content_encoding_kind":"base64","content":"AQI"}`,
		"base64 vs text": `{"bucket":"example-bucket","name":"a","content_encoding_kind":"text","content":"` + over + `"}`,
	} {
		if _, err := handleObjectsInsert(context.Background(), r, s, red, json.RawMessage(raw)); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v", name, err)
		}
		if _, err := e.invokeConfirmed(objectsInsertID, raw); err == nil {
			t.Errorf("%s: invoke accepted", name)
		}
	}
	noIO(t, e, "content checks")
	// Exactly the limit passes.
	e = writeStorage(t)
	if _, err := e.invokeConfirmed(objectsInsertID, `{"bucket":"example-bucket","name":"notes/readme.txt","content":"`+
		over[:maxUploadBytes]+`"}`); err != nil {
		t.Errorf("content of the limit: %v", err)
	}
}

func TestObjectsInsertEncodesTheNameAndTheBucketPath(t *testing.T) {
	e := writeStorage(t)
	e.fake.routes[writeUploadPath] = okReply(objectBody("a/b c#ü?"))
	if _, err := e.invokeConfirmed(objectsInsertID, `{"bucket":"example-bucket","name":"a/b c#ü?","content":"x"}`); err != nil {
		t.Fatal(err)
	}
	post := changes(e.fake.api())
	if len(post) != 1 || !strings.HasPrefix(post[0].url, "https://storage.googleapis.com"+writeUploadPath+"?") {
		t.Fatalf("requests = %+v", post)
	}
	if parts := parseUpload(t, post[0]); parts.metadata["name"] != "a/b c#ü?" {
		t.Errorf("name = %v", parts.metadata["name"])
	}
	e = writeStorage(t)
	const path = bucketRoute + "/o/dir%2Fa%20b%23%C3%BC%3F/compose"
	e.fake.routes[bucketRoute+"/o/dir/a b#ü?/compose"] = okReply(objectBody("dir/a b#ü?"))
	if _, err := e.invokeConfirmed(objectsComposeID, `{"bucket":"example-bucket","destination_object":"dir/a b#ü?",`+
		`"source_objects":[{"name":"x/1"},{"name":"x 2"}]}`); err != nil {
		t.Fatal(err)
	}
	post = changes(e.fake.api())
	if len(post) != 1 || !strings.HasPrefix(post[0].url, "https://storage.googleapis.com"+path) {
		t.Errorf("requests = %+v", post)
	}
}

func TestObjectsWritesRefuseABucketOutsideTheTargetsBeforeTheChange(t *testing.T) {
	for name, c := range map[string][2]string{
		"insert":          {objectsInsertID, `{"bucket":"other-bucket","name":"a","content":"x"}`},
		"copy source":     {objectsCopyID, `{"source_bucket":"other-bucket","source_object":"a","destination_bucket":"example-bucket","destination_object":"b"}`},
		"copy target":     {objectsCopyID, `{"source_bucket":"example-bucket","source_object":"a","destination_bucket":"other-bucket","destination_object":"b"}`},
		"compose":         {objectsComposeID, `{"bucket":"other-bucket","destination_object":"c","source_objects":[{"name":"a"},{"name":"b"}]}`},
		"patch":           {objectsPatchID, `{"bucket":"other-bucket","object":"a","cache_control":"no-cache"}`},
		"unknown project": {objectsInsertID, `{"bucket":"example-bucket","name":"a","content":"x","user_project":"other-project-1"}`},
	} {
		e := writeStorage(t)
		_, err := e.invokeConfirmed(c[0], c[1])
		if err == nil || strings.Contains(err.Error(), "other-bucket") || strings.Contains(err.Error(), foreignNumber) ||
			name != "unknown project" && !isInvalidRequest(err) {
			t.Errorf("%s: err = %v", name, err)
		}
		if len(changes(e.fake.api())) != 0 {
			t.Errorf("%s: a change was sent: %+v", name, e.fake.api())
		}
	}
}

func TestObjectsCopyBindsBothBucketsOneByOneAndBuildsTheBody(t *testing.T) {
	e := writeStorage(t)
	const path = bucketRoute + "/o/dir%2Fsrc%20a/copyTo/b/archive-bucket/o/2026%2Fdst"
	e.fake.routes[bucketRoute+"/o/dir/src a/copyTo/b/archive-bucket/o/2026/dst"] = okReply(strings.ReplaceAll(strings.ReplaceAll(objectBody("2026/dst"),
		`"bucket":"example-bucket"`, `"bucket":"archive-bucket"`), `"size":"11"`, `"size":"11"`))
	if _, err := e.invokeConfirmed(objectsCopyID, `{"source_bucket":"example-bucket","source_object":"dir/src a",`+
		`"source_generation":"123","destination_bucket":"archive-bucket","destination_object":"2026/dst",`+
		`"destination_metadata":{"content_type":"text/plain","metadata":{"k":"v"},"storage_class":"COLDLINE"},`+
		`"destination_predefined_acl":"bucketOwnerRead","destination_kms_key_name":"projects/p/locations/eu/keyRings/r/cryptoKeys/k",`+
		`"if_source_generation_match":"123"}`); err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 3 || api[0].method != http.MethodGet || !strings.Contains(api[0].url, bucketRoute) ||
		api[1].method != http.MethodGet || !strings.Contains(api[1].url, archiveBucket) || api[2].method != http.MethodPost {
		t.Fatalf("requests = %+v", api)
	}
	u, _ := url.Parse(api[2].url)
	if !strings.HasPrefix(api[2].url, "https://storage.googleapis.com"+path+"?") {
		t.Errorf("url = %s", api[2].url)
	}
	for name, want := range map[string]string{"sourceGeneration": "123", "ifSourceGenerationMatch": "123",
		"ifGenerationMatch": "0", "destinationPredefinedAcl": "bucketOwnerRead",
		"destinationKmsKeyName": "projects/p/locations/eu/keyRings/r/cryptoKeys/k"} {
		if got := u.Query().Get(name); got != want {
			t.Errorf("query %s = %q", name, got)
		}
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(api[2].body), &body); err != nil || len(body) != 3 || body["contentType"] != "text/plain" ||
		body["storageClass"] != "COLDLINE" || body["metadata"].(map[string]any)["k"] != "v" {
		t.Errorf("body = %s", api[2].body)
	}
	// The same bucket is read once; a copy without metadata sends no body.
	e = writeStorage(t)
	e.fake.routes[bucketRoute+"/o/a/copyTo/b/example-bucket/o/b"] = okReply(objectBody("b"))
	if _, err := e.invokeConfirmed(objectsCopyID, `{"source_bucket":"example-bucket","source_object":"a",`+
		`"destination_bucket":"example-bucket","destination_object":"b","overwrite":true}`); err != nil {
		t.Fatal(err)
	}
	api = e.fake.api()
	if len(api) != 2 || api[1].body != "" || strings.Contains(api[1].url, "ifGenerationMatch") {
		t.Errorf("requests = %+v", api)
	}
}

func TestObjectsCopyOfAnExistingDestinationFailsWithoutOverwrite(t *testing.T) {
	e := writeStorage(t)
	e.fake.routes[bucketRoute+"/o/a/copyTo/b/example-bucket/o/b"] = reply{status: 412,
		body: errorObject(412, "FAILED_PRECONDITION", "")}
	if _, err := e.invokeConfirmed(objectsCopyID, `{"source_bucket":"example-bucket","source_object":"a",`+
		`"destination_bucket":"example-bucket","destination_object":"b"}`); err == nil ||
		!strings.Contains(err.Error(), "precondition") {
		t.Fatalf("err = %v", err)
	}
	if post := changes(e.fake.api()); len(post) != 1 || !strings.Contains(post[0].url, "ifGenerationMatch=0") {
		t.Errorf("requests = %+v", post)
	}
}

func TestObjectsComposeNeedsTwoToThirtyTwoSourcesAndBuildsTheBody(t *testing.T) {
	source := func(n int) string {
		var items []string
		for i := 0; i < n; i++ {
			items = append(items, `{"name":"part-`+string(rune('a'+i%26))+strings.Repeat("x", i/26)+`"}`)
		}
		return `[` + strings.Join(items, ",") + `]`
	}
	for _, n := range []int{0, 1, 33} {
		e := writeStorage(t)
		args := `{"bucket":"example-bucket","destination_object":"c","source_objects":` + source(n) + `}`
		if _, err := e.invokeConfirmed(objectsComposeID, args); err == nil {
			t.Errorf("%d sources accepted", n)
		}
		noIO(t, e, "compose sources")
		r := writeStorage(t)
		if _, err := handleObjectsCompose(context.Background(), r.resolved(), r.secrets(), r.red, json.RawMessage(args)); !isInvalidRequest(err) {
			t.Errorf("%d sources, handler: %v", n, err)
		}
		noIO(t, r, "compose sources handler")
	}
	for _, n := range []int{2, 32} {
		e := writeStorage(t)
		e.fake.routes[bucketRoute+"/o/c/compose"] = okReply(objectBody("c"))
		if _, err := e.invokeConfirmed(objectsComposeID, `{"bucket":"example-bucket","destination_object":"c",`+
			`"source_objects":`+source(n)+`}`); err != nil {
			t.Fatalf("%d sources: %v", n, err)
		}
	}
	e := writeStorage(t)
	e.fake.routes[bucketRoute+"/o/c/compose"] = okReply(objectBody("c"))
	if _, err := e.invokeConfirmed(objectsComposeID, `{"bucket":"example-bucket","destination_object":"c",`+
		`"source_objects":[{"name":"a","generation":"5","if_generation_match":"5"},{"name":"b"}],`+
		`"content_type":"text/plain","destination_predefined_acl":"projectPrivate"}`); err != nil {
		t.Fatal(err)
	}
	post := changes(e.fake.api())
	if len(post) != 1 || post[0].method != http.MethodPost || !strings.Contains(post[0].url, "ifGenerationMatch=0") ||
		!strings.Contains(post[0].url, "destinationPredefinedAcl=projectPrivate") {
		t.Fatalf("requests = %+v", post)
	}
	var body struct {
		Kind          string `json:"kind"`
		SourceObjects []struct {
			Name                string            `json:"name"`
			Generation          string            `json:"generation"`
			ObjectPreconditions map[string]string `json:"objectPreconditions"`
		} `json:"sourceObjects"`
		Destination map[string]any `json:"destination"`
	}
	if err := json.Unmarshal([]byte(post[0].body), &body); err != nil || body.Kind != "storage#composeRequest" ||
		len(body.SourceObjects) != 2 || body.SourceObjects[0].Name != "a" || body.SourceObjects[0].Generation != "5" ||
		body.SourceObjects[0].ObjectPreconditions["ifGenerationMatch"] != "5" || body.SourceObjects[1].Name != "b" ||
		body.Destination["contentType"] != "text/plain" || len(body.Destination) != 1 ||
		strings.Contains(post[0].body, "deleteSourceObjects") {
		t.Errorf("body = %s (%v)", post[0].body, err)
	}
}

func TestObjectsPatchChangesMetadataAndHoldsWithOneRequest(t *testing.T) {
	e := writeStorage(t)
	e.fake.routes[bucketRoute+"/o/dir/a b"] = okReply(objectBody("dir/a b"))
	if _, err := e.invokeConfirmed(objectsPatchID, `{"bucket":"example-bucket","object":"dir/a b","generation":"42",`+
		`"cache_control":"no-cache","metadata":{"owner":"team"},"remove_metadata_keys":["old"],`+
		`"temporary_hold":true,"event_based_hold":false,"predefined_acl":"private",`+
		`"if_generation_match":"42","if_metageneration_match":"3"}`); err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 2 || api[1].method != http.MethodPatch || !strings.Contains(api[0].url, bucketRoute) {
		t.Fatalf("requests = %+v", api)
	}
	u, _ := url.Parse(api[1].url)
	for name, want := range map[string]string{"generation": "42", "predefinedAcl": "private",
		"ifGenerationMatch": "42", "ifMetagenerationMatch": "3"} {
		if got := u.Query().Get(name); got != want {
			t.Errorf("query %s = %q", name, got)
		}
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(api[1].body), &body); err != nil || len(body) != 4 || body["cacheControl"] != "no-cache" ||
		body["temporaryHold"] != true || body["eventBasedHold"] != false {
		t.Fatalf("body = %s", api[1].body)
	}
	metadata := body["metadata"].(map[string]any)
	if v, present := metadata["old"]; !present || v != nil || metadata["owner"] != "team" {
		t.Errorf("metadata = %v", metadata)
	}
	if _, has := body["acl"]; has {
		t.Errorf("body has an acl: %s", api[1].body)
	}
}

func TestObjectsWritesNeedTheConfirmationOfAPersonAndNameWhatTheyDo(t *testing.T) {
	e := writeStorage(t)
	for id, args := range map[string]string{
		objectsInsertID:  `{"bucket":"example-bucket","name":"a","content":"x"}`,
		objectsCopyID:    `{"source_bucket":"example-bucket","source_object":"a","destination_bucket":"archive-bucket","destination_object":"b"}`,
		objectsComposeID: `{"bucket":"example-bucket","destination_object":"c","source_objects":[{"name":"a"},{"name":"b"}]}`,
		objectsPatchID:   `{"bucket":"example-bucket","object":"a","cache_control":"no-cache"}`,
	} {
		if _, err := e.invoke(id, args); err == nil || !strings.Contains(strings.ToLower(err.Error()), "confirm") {
			t.Errorf("%s ran without confirmation: %v", id, err)
		}
		d, _, _ := e.reg.Lookup(id)
		if d.Risk.Confirmation != "required" || !d.Risk.OpenWorld || d.Risk.DataSensitivity == "" {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
	}
	noIO(t, e, "unconfirmed")
	insert := e.reg.ConfirmationSummary(objectsInsertID)
	for _, want := range []string{`"example-bucket"`, `"a b"`, "3 bytes", "storage class COLDLINE", "creates it and fails"} {
		if got := insert(json.RawMessage(`{"bucket":"example-bucket","name":"a b","content":"abc","storage_class":"COLDLINE"}`)); !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
	got := insert(json.RawMessage(`{"bucket":"example-bucket","name":"a","content":"AQID","content_encoding_kind":"base64","overwrite":true}`))
	for _, want := range []string{"3 bytes", "default class of the bucket", "OVERWRITES"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
	copySummary := e.reg.ConfirmationSummary(objectsCopyID)(json.RawMessage(
		`{"source_bucket":"example-bucket","source_object":"a","destination_bucket":"archive-bucket","destination_object":"b","overwrite":true}`))
	for _, want := range []string{`"example-bucket"`, `"archive-bucket"`, `"a"`, `"b"`, "OVERWRITES"} {
		if !strings.Contains(copySummary, want) {
			t.Errorf("copy summary %q lacks %q", copySummary, want)
		}
	}
	if got := e.reg.ConfirmationSummary(objectsPatchID)(json.RawMessage(
		`{"bucket":"example-bucket","object":"a","temporary_hold":true}`)); !strings.Contains(got, "temporary_hold=true") {
		t.Errorf("patch summary = %q", got)
	}
}

func TestObjectsWritesRefuseWhenTheConfirmationWouldNotFit(t *testing.T) {
	long := strings.Repeat("n", 1000)
	e := writeStorage(t)
	for id, args := range map[string]string{
		objectsInsertID:  `{"bucket":"example-bucket","name":"` + long + `","content":"x"}`,
		objectsCopyID:    `{"source_bucket":"example-bucket","source_object":"` + long + `","destination_bucket":"archive-bucket","destination_object":"` + long + `"}`,
		objectsComposeID: `{"bucket":"example-bucket","destination_object":"` + long + `","source_objects":[{"name":"a"},{"name":"b"}]}`,
		objectsPatchID:   `{"bucket":"example-bucket","object":"` + long + `","cache_control":"no-cache"}`,
	} {
		_, err := e.invokeConfirmed(id, args)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "confirmation") {
			t.Errorf("%s: err = %v", id, err)
		}
		noIO(t, e, id)
	}
	// What fits is shown in full, never cut.
	name := strings.Repeat("ü", 200)
	summary := e.reg.ConfirmationSummary(objectsInsertID)(json.RawMessage(`{"bucket":"example-bucket","name":"` + name + `","content":"x"}`))
	if utf8.RuneCountInString(summary) > maxConfirmationRunes || !strings.Contains(summary, name) {
		t.Errorf("summary = %q", summary)
	}
}

func TestObjectsWritesSayAnUnclearOutcomeAndNeverRepeat(t *testing.T) {
	for _, status := range []int{500, 503} {
		e := writeStorage(t)
		e.fake.routes[writeUploadPath] = reply{status: status, body: errorObject(status, "UNAVAILABLE", "")}
		_, err := e.invokeConfirmed(objectsInsertID, `{"bucket":"example-bucket","name":"a","content":"body-canary"}`)
		if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
			strings.Contains(err.Error(), "body-canary") {
			t.Errorf("status %d: err = %v", status, err)
		}
		if post := changes(e.fake.api()); len(post) != 1 {
			t.Errorf("status %d: %d change requests", status, len(post))
		}
	}
	e := writeStorage(t)
	e.fake.routes[bucketRoute+"/o/a"] = reply{status: 502, body: errorObject(502, "UNAVAILABLE", "")}
	if _, err := e.invokeConfirmed(objectsPatchID, `{"bucket":"example-bucket","object":"a","cache_control":"x"}`); err == nil ||
		!strings.Contains(err.Error(), "unknown") || len(changes(e.fake.api())) != 1 {
		t.Errorf("patch: err = %v, requests %+v", err, e.fake.api())
	}
}

func TestObjectsWriteAnswersOfAnotherObjectAreInvalid(t *testing.T) {
	e := writeStorage(t)
	e.fake.routes[writeUploadPath] = okReply(objectBody("other-name"))
	if _, err := e.invokeConfirmed(objectsInsertID, `{"bucket":"example-bucket","name":"a","content":"x"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func TestObjectsWriteToolsAreOnAConnectionWithoutTargetsToo(t *testing.T) {
	e := createEnvironment(t, nil, objectWriteTools)
	e.fake.routes[writeUploadPath] = okReply(objectBody("notes/readme.txt"))
	if _, err := e.invokeConfirmed(objectsInsertID, `{"bucket":"example-bucket","name":"notes/readme.txt","content":"x"}`); err != nil {
		t.Fatal(err)
	}
	if api := e.fake.api(); len(api) != 1 || api[0].method != http.MethodPost {
		t.Errorf("requests = %+v", api)
	}
}

func queryOf(t *testing.T, r seen) url.Values {
	t.Helper()
	u, err := url.Parse(r.url)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

func TestObjectsWritesSendTheGuardsProjectionAndContextsOfEveryMethod(t *testing.T) {
	// objects.insert
	e := writeStorage(t)
	if _, err := e.invokeConfirmed(objectsInsertID, `{"bucket":"example-bucket","name":"notes/readme.txt","content":"x",`+
		`"projection":"full","if_generation_not_match":"9","if_metageneration_not_match":"8",`+
		`"contexts":{"team":"web"}}`); err != nil {
		t.Fatal(err)
	}
	post := changes(e.fake.api())
	q := queryOf(t, post[0])
	if q.Get("projection") != "full" || q.Get("ifGenerationNotMatch") != "9" || q.Get("ifMetagenerationNotMatch") != "8" ||
		q.Get("ifGenerationMatch") != "0" {
		t.Errorf("insert query = %v", q)
	}
	parts := parseUpload(t, post[0])
	if parts.metadata["contexts"].(map[string]any)["custom"].(map[string]any)["team"].(map[string]any)["value"] != "web" {
		t.Errorf("insert contexts = %v", parts.metadata)
	}
	// objects.copy
	e = writeStorage(t)
	e.fake.routes[bucketRoute+"/o/a/copyTo/b/example-bucket/o/b"] = okReply(objectBody("b"))
	if _, err := e.invokeConfirmed(objectsCopyID, `{"source_bucket":"example-bucket","source_object":"a",`+
		`"destination_bucket":"example-bucket","destination_object":"b","projection":"noAcl",`+
		`"if_generation_not_match":"1","if_metageneration_match":"2","if_metageneration_not_match":"3",`+
		`"if_source_generation_not_match":"4","if_source_metageneration_match":"5",`+
		`"if_source_metageneration_not_match":"6","destination_metadata":{"contexts":{"k":"v"}}}`); err != nil {
		t.Fatal(err)
	}
	post = changes(e.fake.api())
	q = queryOf(t, post[0])
	for name, want := range map[string]string{"projection": "noAcl", "ifGenerationNotMatch": "1",
		"ifMetagenerationMatch": "2", "ifMetagenerationNotMatch": "3", "ifSourceGenerationNotMatch": "4",
		"ifSourceMetagenerationMatch": "5", "ifSourceMetagenerationNotMatch": "6", "ifGenerationMatch": "0"} {
		if q.Get(name) != want {
			t.Errorf("copy query %s = %q", name, q.Get(name))
		}
	}
	if !strings.Contains(post[0].body, `"contexts":{"custom":{"k":{"value":"v"}}}`) {
		t.Errorf("copy body = %s", post[0].body)
	}
	// objects.compose
	e = writeStorage(t)
	e.fake.routes[bucketRoute+"/o/c/compose"] = okReply(objectBody("c"))
	if _, err := e.invokeConfirmed(objectsComposeID, `{"bucket":"example-bucket","destination_object":"c",`+
		`"source_objects":[{"name":"a"},{"name":"b"}],"drop_context_groups":["custom"],`+
		`"if_metageneration_match":"7","contexts":{"k":"v"}}`); err != nil {
		t.Fatal(err)
	}
	post = changes(e.fake.api())
	q = queryOf(t, post[0])
	if q.Get("dropContextGroups") != "custom" || q.Get("ifMetagenerationMatch") != "7" || q.Get("ifGenerationMatch") != "0" ||
		!strings.Contains(post[0].body, `"destination":{"contentType":"application/octet-stream","contexts":{"custom":{"k":{"value":"v"}}}}`) {
		t.Errorf("compose = %s %v", post[0].body, q)
	}
	// objects.patch
	e = writeStorage(t)
	e.fake.routes[bucketRoute+"/o/a"] = okReply(objectBody("a"))
	if _, err := e.invokeConfirmed(objectsPatchID, `{"bucket":"example-bucket","object":"a","projection":"full",`+
		`"if_generation_not_match":"1","if_metageneration_not_match":"2","contexts":{"k":"v"}}`); err != nil {
		t.Fatal(err)
	}
	post = changes(e.fake.api())
	q = queryOf(t, post[0])
	if q.Get("projection") != "full" || q.Get("ifGenerationNotMatch") != "1" || q.Get("ifMetagenerationNotMatch") != "2" ||
		!strings.Contains(post[0].body, `"contexts":{"custom":{"k":{"value":"v"}}}`) {
		t.Errorf("patch = %s %v", post[0].body, q)
	}
}

func TestObjectsWritesRefuseBadProjectionGuardsAndContextsBeforeIO(t *testing.T) {
	for name, c := range map[string][2]string{
		"insert projection":  {objectsInsertID, `{"bucket":"example-bucket","name":"a","content":"x","projection":"all"}`},
		"insert guard":       {objectsInsertID, `{"bucket":"example-bucket","name":"a","content":"x","if_generation_not_match":"x"}`},
		"copy projection":    {objectsCopyID, `{"source_bucket":"example-bucket","source_object":"a","destination_bucket":"example-bucket","destination_object":"b","projection":"all"}`},
		"copy source guard":  {objectsCopyID, `{"source_bucket":"example-bucket","source_object":"a","destination_bucket":"example-bucket","destination_object":"b","if_source_metageneration_match":"-1"}`},
		"compose drop group": {objectsComposeID, `{"bucket":"example-bucket","destination_object":"c","source_objects":[{"name":"a"},{"name":"b"}],"drop_context_groups":["system"]}`},
		"patch projection":   {objectsPatchID, `{"bucket":"example-bucket","object":"a","cache_control":"x","projection":"all"}`},
		"patch contexts":     {objectsPatchID, `{"bucket":"example-bucket","object":"a","contexts":{"k":"` + strings.Repeat("v", 1025) + `"}}`},
	} {
		e := writeStorage(t)
		if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
			t.Errorf("%s: accepted", name)
		}
		noIO(t, e, name)
	}
}
