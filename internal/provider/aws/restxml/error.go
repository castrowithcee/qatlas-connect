package restxml

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/awserr"
)

var errorToken = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

// errorFields reads the Code and Type of an error body, in the form the service's noErrorWrapping property
// says: an Error element (S3), or an ErrorResponse element holding an Error element (Route 53). For the wrapped
// form it also takes the bare element named by the code that Route 53 sends for InvalidChangeBatch, only when
// awserr knows the name as a code. found says that
// the body had one of these forms. Neither the Message nor any other text is read.
func (c Codec) errorFields(body []byte) (code, errType string, found bool) {
	root, err := parse(body)
	if err != nil {
		return "", "", false
	}
	holder := root
	switch {
	case root.name == "Error" && c.NoErrorWrapping:
	case root.name == "ErrorResponse" && !c.NoErrorWrapping:
		if holder, err = root.child("Error"); err != nil || holder == nil {
			return "", "", false
		}
	case !c.NoErrorWrapping && awserr.Defines(root.name):
		return root.name, "", true
	default:
		return "", "", false
	}
	text := func(name string) string {
		kid, err := holder.child(name)
		if err != nil || kid == nil || len(kid.kids) > 0 {
			return ""
		}
		v := strings.TrimSpace(kid.text.String())
		if len(v) > maxErrorCode || !errorToken.MatchString(v) {
			return ""
		}
		return v
	}
	return text("Code"), text("Type"), true
}

// ErrorCode implements httpbind.Codec. A body that is not one of the error forms, or has no usable Code, gives
// no code, and the status decides alone.
func (c Codec) ErrorCode(_ http.Header, body []byte) string {
	code, _, _ := c.errorFields(body)
	return code
}

// ErrorType implements httpbind.ErrorTyper: the Type of a Route 53 error, Sender or Receiver.
func (c Codec) ErrorType(_ http.Header, body []byte) string {
	_, errType, _ := c.errorFields(body)
	return errType
}

// SuccessError implements httpbind.SuccessErrorDetector. Only a service that does not wrap its errors (S3) can
// answer one with a success status, and only an Error element at the root is one. A body that is not
// XML is no error; the decoder judges it.
func (c Codec) SuccessError(body []byte) (code, errType string, found bool) {
	if !c.NoErrorWrapping || !looksLikeError(body) {
		return "", "", false
	}
	return c.errorFields(body)
}

// looksLikeError is a cheap test before the body is parsed: a document whose root element is Error.
func looksLikeError(body []byte) bool {
	s := strings.TrimLeft(string(body[:min(len(body), 512)]), " \t\r\n")
	if rest, ok := strings.CutPrefix(s, "<?xml"); ok {
		_, after, found := strings.Cut(rest, "?>")
		if !found {
			return false
		}
		s = strings.TrimLeft(after, " \t\r\n")
	}
	return strings.HasPrefix(s, "<Error>") || strings.HasPrefix(s, "<Error ") || strings.HasPrefix(s, "<Error/>")
}

// ClassifyError implements httpbind.ErrorClassifier.
func (Codec) ClassifyError(op string, status int, header http.Header, code, errType string) (*provider.Error, time.Duration) {
	return awserr.ClassifyXML(op, status, header, code, errType)
}
