package httpbind

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestBucketNames(t *testing.T) {
	for name, virtual := range map[string]bool{
		"abc": true, "my-bucket": true, "a1b2": true, "0abc": true, strings.Repeat("a", 63): true,
		"my.bucket": false, "MyBucket": false, "my_bucket": false, "a.b.c": false, "abc.": false,
	} {
		want := name != "abc."
		if got := ValidBucketName(name); got != want {
			t.Errorf("ValidBucketName(%q) = %t", name, got)
		}
		if got := VirtualHostable(name); got != (virtual && want) {
			t.Errorf("VirtualHostable(%q) = %t", name, got)
		}
	}
	for _, bad := range []string{"", "a", "ab", strings.Repeat("a", 64), "-abc", "abc-", ".abc", "_abc", "a..b", "a.-b", "a-.b", "a b", "a/b", "a%2Fb",
		"a\nb", "a:b", "a?b", "a#b", "ä-bucket", "192.168.5.4", "1.2.3.4", "xn--abc", "sthree-abc", "sthree-configurator-x", "amzn-s3-demo-b",
		"abc-s3alias", "abc--ol-s3", "abc.mrap", "abc--x-s3", "abc--table-s3", "arn:aws:s3:::abc"} {
		if ValidBucketName(bad) || VirtualHostable(bad) {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func bucketOp() *Operation {
	str := &Shape{Kind: KindString}
	return &Operation{Method: "PUT", URI: "/{Bucket}/{Key+}", ChecksumRequired: false,
		Input: &Shape{Kind: KindStruct, Fields: []Field{
			{Key: "Bucket", Name: "Bucket", Shape: str, Loc: LocLabel, Bucket: true},
			{Key: "Key", Name: "Key", Shape: str, Loc: LocLabel},
			{Key: "Owner", Name: "x-amz-expected-bucket-owner", Shape: str, Loc: LocHeader, Owner: true},
			{Key: "Type", Name: "Content-Type", Shape: str, Loc: LocHeader, ContentType: true},
			{Key: "Body", Name: "Body", Shape: &Shape{Kind: KindBlob}, Loc: LocPayload, Stream: true}}},
		Output: &Shape{Kind: KindStruct, Fields: []Field{{Key: "ETag", Name: "ETag", Shape: str, Loc: LocHeader}}}}
}

type textCodec struct{}

func (textCodec) Marshal(*Shape, any) ([]byte, error)         { return nil, nil }
func (textCodec) Unmarshal(*Shape, []byte) (any, bool, error) { return nil, false, nil }
func (textCodec) ContentType() string                         { return "application/xml" }
func (textCodec) Accept() string                              { return "application/xml" }
func (textCodec) ErrorCode(http.Header, []byte) string        { return "" }

func TestEncodeWithTheLayerRoles(t *testing.T) {
	o := bucketOp()
	req, err := o.EncodeWith(textCodec{}, map[string]any{"Bucket": "my-bucket", "Key": "a/b", "Type": "text/plain; charset=utf-8"},
		EncodeOptions{Owner: "123456789012"})
	if err != nil || req.Bucket != "my-bucket" || req.Header.Get("X-Amz-Expected-Bucket-Owner") != "123456789012" ||
		req.Header.Get("Content-Type") != "text/plain; charset=utf-8" || req.RawPath != "/my-bucket/a/b" {
		t.Fatalf("request = %+v, %v", req, err)
	}
	// Without an owner no header is sent; with a malformed one nothing is encoded.
	req, err = o.EncodeWith(textCodec{}, map[string]any{"Bucket": "my-bucket", "Key": "k"}, EncodeOptions{})
	if err != nil || req.Header.Get("X-Amz-Expected-Bucket-Owner") != "" || req.Header.Get("Content-Type") != "" {
		t.Errorf("no options: %+v, %v", req, err)
	}
	for _, owner := range []string{"1234", "abcdefghijkl", "1234567890123", "123456789012\n"} {
		if _, err := o.EncodeWith(textCodec{}, map[string]any{"Bucket": "my-bucket", "Key": "k"}, EncodeOptions{Owner: owner}); !errors.Is(err, ErrValue) {
			t.Errorf("owner %q: %v", owner, err)
		}
	}
	// Owner and stream members are not arguments.
	for _, args := range []map[string]any{{"Owner": "1"}, {"Body": "aGk="}} {
		args["Bucket"], args["Key"] = "my-bucket", "k"
		if _, err := o.EncodeWith(textCodec{}, args, EncodeOptions{}); !errors.Is(err, ErrValue) || !strings.Contains(err.Error(), "unknown argument") {
			t.Errorf("%v: %v", args, err)
		}
	}
	for _, bad := range []string{"text", "text/", "/plain", "text/plain\r\nX-Evil: 1", "text/plain; x", "text/pl ain", strings.Repeat("a", 300) + "/b", "text/plain;a=b;c"} {
		if _, err := o.EncodeWith(textCodec{}, map[string]any{"Bucket": "my-bucket", "Key": "k", "Type": bad}, EncodeOptions{}); !errors.Is(err, ErrValue) {
			t.Errorf("media type %q: %v", bad, err)
		}
	}
	for _, bad := range []string{"a/b", "ab", "A..b", "../x"} {
		if _, err := o.EncodeWith(textCodec{}, map[string]any{"Bucket": bad, "Key": "k"}, EncodeOptions{}); !errors.Is(err, ErrValue) {
			t.Errorf("bucket %q: %v", bad, err)
		}
	}
}

func TestChecksumHeadersOfTheLayer(t *testing.T) {
	str := &Shape{Kind: KindString}
	o := &Operation{Method: "PUT", URI: "/{Bucket}?policy", ChecksumRequired: true, ValidateResponseChecksum: true,
		Input: &Shape{Kind: KindStruct, Fields: []Field{{Key: "Bucket", Name: "Bucket", Shape: str, Loc: LocLabel, Bucket: true},
			{Key: "Policy", Name: "Policy", Shape: str, Loc: LocPayload}}}}
	req, err := o.Encode(textCodec{}, map[string]any{"Bucket": "my-bucket", "Policy": "{}"}, nil)
	if err != nil || req.Header.Get("X-Amz-Checksum-Sha256") != "RBNvo1WzZ4oRRq0W9+hknpT7T8If536DEMBg9hyq/4o=" ||
		req.Header.Get("X-Amz-Sdk-Checksum-Algorithm") != "SHA256" || req.Header.Get("X-Amz-Checksum-Mode") != "ENABLED" {
		t.Errorf("headers = %v, %v", req.Header, err)
	}
	// No body, no checksum of it.
	bodyless := &Operation{Method: "GET", URI: "/{Bucket}", ChecksumRequired: true,
		Input: &Shape{Kind: KindStruct, Fields: []Field{{Key: "Bucket", Name: "Bucket", Shape: str, Loc: LocLabel}}}}
	if req, err := bodyless.Encode(textCodec{}, map[string]any{"Bucket": "my-bucket"}, nil); err != nil || req.Header.Get("X-Amz-Checksum-Sha256") != "" {
		t.Errorf("bodyless = %v, %v", req.Header, err)
	}
	// A streamed payload cannot carry the required checksum, so the description is refused.
	streamed := bucketOp()
	streamed.ChecksumRequired = true
	if err := streamed.Validate(); !errors.Is(err, ErrShape) {
		t.Errorf("streamed with a required checksum: %v", err)
	}
}

func TestLayerRolesAreChecked(t *testing.T) {
	str := &Shape{Kind: KindString}
	cases := map[string]Field{
		"bucket as a query":  {Key: "B", Name: "b", Shape: str, Loc: LocQuery, Bucket: true},
		"owner as a label":   {Key: "O", Name: "O", Shape: str, Loc: LocLabel, Owner: true},
		"owner required":     {Key: "O", Name: "x-o", Shape: str, Loc: LocHeader, Owner: true, Required: true},
		"stream as a string": {Key: "S", Name: "S", Shape: str, Loc: LocPayload, Stream: true},
		"type elsewhere":     {Key: "T", Name: "X-Type", Shape: str, Loc: LocHeader, ContentType: true},
		"two roles":          {Key: "X", Name: "x", Shape: str, Loc: LocHeader, Owner: true, ContentType: true},
	}
	for name, f := range cases {
		op := &Operation{Method: "PUT", URI: "/x", Input: &Shape{Kind: KindStruct, Fields: []Field{f}}}
		if f.Loc == LocLabel {
			op.URI = "/{O}"
		}
		if err := op.Validate(); !errors.Is(err, ErrShape) {
			t.Errorf("%s: %v", name, err)
		}
	}
	nested := &Operation{Method: "PUT", URI: "/x", Input: &Shape{Kind: KindStruct, Fields: []Field{
		{Key: "D", Name: "D", Shape: &Shape{Kind: KindStruct, Fields: []Field{{Key: "N", Name: "N", Shape: str, Bucket: true}}}}}}}
	if err := nested.Validate(); !errors.Is(err, ErrShape) {
		t.Errorf("nested role: %v", err)
	}
}
