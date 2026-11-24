package aws

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// Raw MIME content of SendEmail. The tool neither signs nor changes it and does not read what an attachment
// holds; it refuses the headers that would let the content decide who gets the mail or who is shown as its
// sender, and names sender, subject, and attachments in the confirmation.

const (
	// sesMaxRawBase64 caps the base64 text of the message (about 7.5 MB decoded), well under the 40 MB of SES.
	sesMaxRawBase64 = 10_000_000
	// sesMaxAttachments caps the attachments of all multipart levels together.
	sesMaxAttachments = 10
	// sesMaxMIMEDepth and sesMaxMIMEParts bound the work a hostile message of nested parts can cause.
	sesMaxMIMEDepth, sesMaxMIMEParts = 5, 200
)

// rawMessage is what the checks and the confirmation read from the MIME content.
type rawMessage struct {
	from, subject string
	attachments   []string
}

var errRawUnreadable = errors.New("unreadable MIME content")

// parseRawMessage reads base64 MIME content. Only the headers of the top level count, since they are the
// ones SES sends with; the parts below are only walked to count and name the attachments.
func parseRawMessage(data string) (rawMessage, error) {
	var out rawMessage
	if len(data) > sesMaxRawBase64 {
		return out, errRawUnreadable
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return out, errRawUnreadable
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return out, errRawUnreadable
	}
	var froms []string
	for key, values := range msg.Header {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "from":
			froms = append(froms, values...)
		case "bcc", "return-path":
			return out, errRawUnreadable
		}
	}
	if len(froms) != 1 || strings.TrimSpace(froms[0]) == "" {
		return out, errRawUnreadable
	}
	out.from = decodeWords(froms[0])
	out.subject = decodeWords(msg.Header.Get("Subject"))
	parts := 0
	err = walkParts(msg.Header.Get("Content-Type"), msg.Body, 0, &parts, &out.attachments)
	return out, err
}

func decodeWords(s string) string {
	if d, err := new(mime.WordDecoder).DecodeHeader(s); err == nil {
		return d
	}
	return s
}

// walkParts counts the attachments of a body and of every multipart level below it.
func walkParts(contentType string, body io.Reader, depth int, parts *int, names *[]string) error {
	if strings.TrimSpace(contentType) == "" {
		return nil
	}
	media, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return errRawUnreadable
	}
	if !strings.HasPrefix(media, "multipart/") {
		return nil
	}
	if depth >= sesMaxMIMEDepth || params["boundary"] == "" {
		return errRawUnreadable
	}
	r := multipart.NewReader(body, params["boundary"])
	for {
		part, err := r.NextPart()
		if err == io.EOF {
			return nil
		}
		if *parts++; err != nil || *parts > sesMaxMIMEParts {
			return errRawUnreadable
		}
		if err := walkParts(part.Header.Get("Content-Type"), part, depth+1, parts, names); err != nil {
			return err
		}
		disposition, dparams, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		name := dparams["filename"]
		if name == "" {
			_, cparams, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
			name = cparams["name"]
		}
		if disposition == "attachment" || name != "" {
			if *names = append(*names, decodeWords(name)); len(*names) > sesMaxAttachments {
				return errRawUnreadable
			}
		}
	}
}

// checkSESRaw judges the raw content of a send; it reports nothing of the content itself.
func checkSESRaw(content map[string]any) error {
	rawArg, set := content["raw"]
	if !set {
		return nil
	}
	if content["simple"] != nil || content["template"] != nil {
		return &provider.InvalidRequestError{Message: "content takes exactly one of simple, template, or raw"}
	}
	rawContent, _ := rawArg.(map[string]any)
	data, _ := rawContent["data"].(string)
	if _, err := parseRawMessage(data); err != nil {
		return &provider.InvalidRequestError{Message: "content.raw.data must be base64 MIME content of at most 10 MB with exactly one From header, " +
			"no Bcc and no Return-Path header, at most 10 attachments, and well-formed multipart parts"}
	}
	return nil
}
