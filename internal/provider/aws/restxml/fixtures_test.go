package restxml_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/httpbind"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/restxml"
)

type (
	shape = httpbind.Shape
	field = httpbind.Field
)

func str() *shape   { return &shape{Kind: httpbind.KindString} }
func num() *shape   { return &shape{Kind: httpbind.KindInteger} }
func flag() *shape  { return &shape{Kind: httpbind.KindBoolean} }
func blob() *shape  { return &shape{Kind: httpbind.KindBlob} }
func stamp() *shape { return &shape{Kind: httpbind.KindTimestamp} }
func list(e *shape) *shape {
	return &shape{Kind: httpbind.KindList, Elem: e}
}
func named(e *shape, name string) *shape {
	e.ElemName = name
	return e
}
func mapOf(e *shape) *shape { return &shape{Kind: httpbind.KindMap, Elem: e} }
func obj(fields ...field) *shape {
	return &shape{Kind: httpbind.KindStruct, Fields: fields}
}
func root(name string, s *shape) *shape {
	s.XMLName = name
	return s
}
func f(key, name string, s *shape) field { return field{Key: key, Name: name, Shape: s} }
func at(loc httpbind.Loc, fl field) field {
	fl.Loc = loc
	return fl
}
func flat(fl field) field {
	fl.Flattened = true
	return fl
}
func attrOf(fl field) field {
	fl.Attribute = true
	return fl
}

// wire is one request as the fake endpoint saw it.
type wire struct {
	method, path, rawQuery, body, host string
	header                             http.Header
}

// exchange is a Doer that records the request and answers.
type exchange struct {
	got    []wire
	status int
	header map[string]string
	body   string
	err    error
}

func (e *exchange) Do(req *http.Request) (*http.Response, error) {
	data, _ := io.ReadAll(req.Body)
	e.got = append(e.got, wire{req.Method, req.URL.EscapedPath(), req.URL.RawQuery, string(data), req.URL.Host, req.Header.Clone()})
	if e.err != nil {
		return nil, e.err
	}
	h := http.Header{}
	for k, v := range e.header {
		h.Set(k, v)
	}
	status := e.status
	if status == 0 {
		status = 200
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(e.body))}, nil
}

type signerFunc func(*http.Request, []byte) error

func (s signerFunc) Sign(r *http.Request, b []byte) error { return s(r, b) }

func newClient(e *exchange, noWrap bool) *httpbind.Client {
	return restxml.NewClient(httpbind.Client{
		HTTP: e, URL: "https://s3.eu-central-1.amazonaws.com", UserAgent: "qatlas/test",
		Signer: signerFunc(func(r *http.Request, _ []byte) error { r.Header.Set("Authorization", "signed"); return nil }),
	}, noWrap)
}

// call encodes and sends one operation against e.
func call(t *testing.T, e *exchange, noWrap bool, o *httpbind.Operation, args map[string]any) (map[string]any, httpbind.Meta, *provider.Error) {
	t.Helper()
	req, err := o.Encode(restxml.Codec{NoErrorWrapping: noWrap}, args, nil)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var out map[string]any
	meta, failure := newClient(e, noWrap).Do(context.Background(), "aws.test", o, req, &out)
	return out, meta, failure
}
