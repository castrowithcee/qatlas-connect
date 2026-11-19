package aws

import (
	"encoding/json"
	"strconv"
	"strings"
)

// The confirmation texts of the SES account write tools. The core bounds, redacts, and neutralizes the text.

const summaryContactsShown = 2

func init() {
	registerSummary("aws.ses.account.put-details", accountDetailsSummary)
	registerSummary("aws.ses.account.put-sending-attributes", accountSendingSummary)
	registerSummary("aws.ses.account.put-suppression-attributes", accountSuppressionSummary)
	registerSummary("aws.ses.account.put-dedicated-ip-warmup-attributes", accountWarmupSummary)
	registerSummary("aws.ses.account.put-vdm-attributes", accountVdmSummary)
}

func summaryOrNone(v string) string {
	if v == "" {
		return "not set"
	}
	return summaryValue(v)
}

func onOff(b *bool) string {
	if b != nil && *b {
		return "on"
	}
	return "off"
}

func accountDetailsSummary(raw json.RawMessage) string {
	var a struct {
		Region     string   `json:"region"`
		MailType   string   `json:"mail_type"`
		Website    string   `json:"website_url"`
		Language   string   `json:"contact_language"`
		Contacts   []string `json:"additional_contact_email_addresses"`
		Production *bool    `json:"production_access_enabled"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	out := "Submit the account details of SES " + summaryRegion(a.Region) + ": mail type " + summaryValue(a.MailType) +
		", website " + summaryValue(a.Website) + ", contact language " + summaryOrNone(a.Language) + "."
	if len(a.Contacts) == 0 {
		out += " No additional contact addresses."
	} else {
		shown := a.Contacts
		if len(shown) > summaryContactsShown {
			shown = shown[:summaryContactsShown]
		}
		names := make([]string, len(shown))
		for i, c := range shown {
			names[i] = summaryValue(c)
		}
		out += " " + strconv.Itoa(len(a.Contacts)) + " additional contact address(es) go to AWS only and receive no mail: " +
			strings.Join(names, ", ")
		if rest := len(a.Contacts) - len(shown); rest > 0 {
			out += " and " + strconv.Itoa(rest) + " more"
		}
		out += "."
	}
	if a.Production != nil && *a.Production {
		out += " This asks for production access."
	} else {
		out += " This does not ask for production access."
	}
	return out + " AWS reviews the request, and the decision can take a while."
}

func accountSendingSummary(raw json.RawMessage) string {
	var a struct {
		Region  string `json:"region"`
		Enabled *bool  `json:"sending_enabled"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	out := "Turn sending of the whole SES account " + summaryRegion(a.Region) + " " + onOff(a.Enabled) + "."
	if a.Enabled == nil || !*a.Enabled {
		out += " All mail of the account there stops at once."
	}
	return out
}

func accountSuppressionSummary(raw json.RawMessage) string {
	var a struct {
		Region     string   `json:"region"`
		Reasons    []string `json:"suppressed_reasons"`
		Validation *struct {
			Threshold struct {
				Enabled string `json:"condition_threshold_enabled"`
				Overall struct {
					Verdict string `json:"confidence_verdict_threshold"`
				} `json:"overall_confidence_threshold"`
			} `json:"condition_threshold"`
		} `json:"validation_attributes"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	reasons := "none"
	if len(a.Reasons) > 0 {
		names := make([]string, len(a.Reasons))
		for i, r := range a.Reasons {
			names[i] = summaryValue(r)
		}
		reasons = strings.Join(names, ", ")
	}
	out := "Set the reasons that add an address to the account suppression list of SES " + summaryRegion(a.Region) + " to: " + reasons + "."
	if v := a.Validation; v != nil {
		out += " Validation threshold " + summaryOrNone(v.Threshold.Enabled)
		if v.Threshold.Overall.Verdict != "" {
			out += " at confidence " + summaryValue(v.Threshold.Overall.Verdict)
		}
		out += "."
	}
	return out
}

func accountWarmupSummary(raw json.RawMessage) string {
	var a struct {
		Region  string `json:"region"`
		Enabled *bool  `json:"auto_warmup_enabled"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return "Turn the automatic warm-up of new dedicated IP addresses of SES " + summaryRegion(a.Region) + " " + onOff(a.Enabled) + "."
}

func accountVdmSummary(raw json.RawMessage) string {
	var a struct {
		Region string `json:"region"`
		Vdm    struct {
			Enabled   string `json:"vdm_enabled"`
			Dashboard struct {
				Engagement string `json:"engagement_metrics"`
			} `json:"dashboard_attributes"`
			Guardian struct {
				Shared string `json:"optimized_shared_delivery"`
			} `json:"guardian_attributes"`
		} `json:"vdm_attributes"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	v := a.Vdm
	out := "Set Virtual Deliverability Manager of SES " + summaryRegion(a.Region) + " to " + summaryOrNone(v.Enabled) +
		" (dashboard engagement metrics " + summaryOrNone(v.Dashboard.Engagement) + ", guardian optimized shared delivery " +
		summaryOrNone(v.Guardian.Shared) + ")."
	if v.Enabled == "ENABLED" {
		out += " Virtual Deliverability Manager costs extra on top of the sending price."
	}
	return out
}
