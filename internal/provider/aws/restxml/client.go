package restxml

import "github.com/castrowithcee/qatlas-connect/internal/provider/aws/httpbind"

// NewClient returns the REST client with the restXml body format. noErrorWrapping is the service's
// restXml property (true for S3, false for Route 53). The caller fills HTTP, Signer and URL (the endpoint of
// the table); the host never comes from a request.
func NewClient(base httpbind.Client, noErrorWrapping bool) *httpbind.Client {
	base.Codec = Codec{NoErrorWrapping: noErrorWrapping}
	return &base
}
