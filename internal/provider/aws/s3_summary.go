package aws

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The confirmation texts of the S3 write tools. The core bounds, redacts, and neutralizes the text; the limits
// here only keep one long argument from pushing the effect out of it.

const summaryValueRunes = 128

func init() {
	registerSummary("aws.s3.object-lock.put", objectLockSummary)
	registerSummary("aws.s3.object-retention.put", objectRetentionSummary)
	registerSummary("aws.s3.object-legal-hold.put", objectLegalHoldSummary)
	registerSummary("aws.s3.request-payment.put", requestPaymentSummary)
}

// summaryValue cuts an untrusted argument to a few runes and drops control characters.
func summaryValue(v string) string {
	v = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return ' '
		}
		return r
	}, v)
	if r := []rune(v); len(r) > summaryValueRunes {
		return string(r[:summaryValueRunes]) + "…"
	}
	return v
}

// summaryRegion names the region of the call. Without the argument the call goes to the one region the
// connection allows, which is not known here.
func summaryRegion(region string) string {
	if region == "" {
		return "in the one region the connection allows"
	}
	return "in region " + summaryValue(region)
}

type summaryTarget struct {
	Bucket    string `json:"bucket"`
	Region    string `json:"region"`
	Key       string `json:"key"`
	VersionID string `json:"version_id"`
}

func (t summaryTarget) bucket() string {
	return "bucket " + summaryValue(t.Bucket) + " " + summaryRegion(t.Region)
}

func (t summaryTarget) object() string {
	out := "object " + summaryValue(t.Key)
	if t.VersionID != "" {
		return out + " (version " + summaryValue(t.VersionID) + ")"
	}
	return out + " (current version)"
}

func objectLockSummary(raw json.RawMessage) string {
	var a struct {
		summaryTarget
		Config struct {
			Enabled string `json:"object_lock_enabled"`
			Rule    struct {
				Default struct {
					Mode  string      `json:"mode"`
					Days  json.Number `json:"days"`
					Years json.Number `json:"years"`
				} `json:"default_retention"`
			} `json:"rule"`
		} `json:"object_lock_configuration"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	out := "Turn on Object Lock for " + a.bucket() + ": this cannot be undone, Object Lock stays on for the life of the bucket."
	d := a.Config.Rule.Default
	switch {
	case d.Mode == "":
		out += " No default retention is set."
	default:
		period := "an unreadable period"
		switch {
		case d.Days != "":
			period = summaryValue(d.Days.String()) + " days"
		case d.Years != "":
			period = summaryValue(d.Years.String()) + " years"
		}
		out += " Every new object gets a default retention in mode " + summaryValue(d.Mode) + " for " + period +
			" and cannot be deleted or overwritten until it ends."
	}
	return out
}

func objectRetentionSummary(raw json.RawMessage) string {
	var a struct {
		summaryTarget
		Retention struct {
			Mode  string `json:"mode"`
			Until string `json:"retain_until_date"`
		} `json:"retention"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return "Retain " + a.object() + " in " + a.bucket() + " in mode " + summaryValue(a.Retention.Mode) +
		" until " + summaryValue(a.Retention.Until) + ": until then it cannot be deleted or overwritten, and the date can be " +
		"extended but not moved earlier."
}

func objectLegalHoldSummary(raw json.RawMessage) string {
	var a struct {
		summaryTarget
		Hold struct {
			Status string `json:"status"`
		} `json:"legal_hold"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	out := "Set the legal hold of " + a.object() + " in " + a.bucket() + " to " + summaryValue(a.Hold.Status) + ": "
	if a.Hold.Status == "OFF" {
		return out + "the hold is removed, other retention of the object still applies."
	}
	return out + "the object cannot be deleted or overwritten until the hold is set OFF, it has no end date."
}

func requestPaymentSummary(raw json.RawMessage) string {
	var a struct {
		summaryTarget
		Config struct {
			Payer string `json:"payer"`
		} `json:"request_payment_configuration"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	out := "Set the payer of " + a.bucket() + " to " + summaryValue(a.Config.Payer) + ": "
	if a.Config.Payer == "Requester" {
		return out + "the costs of transfer and requests move to whoever asks, anonymous access is no longer possible, " +
			"and requests of other accounts fail unless they send x-amz-request-payer."
	}
	return out + "the bucket owner pays for transfer and requests again. This can be reverted at any time."
}
