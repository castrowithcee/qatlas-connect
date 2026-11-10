package aws

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	jobID    = "01234567-89ab-cdef-0123-456789abcdef"
	jobExtra = `<JobStatus>COMPLETED</JobStatus><JobCreationDate>2026-01-02T03:04:05Z</JobCreationDate>` +
		`<JobCompletionDate>2026-01-02T03:05:05Z</JobCompletionDate>`
	generatedAt = "2026-01-02T03:04:05Z"

	reportHeader = "user,arn,user_creation_time,password_enabled,password_last_used,password_last_changed,password_next_rotation," +
		"mfa_active,access_key_1_active,access_key_1_last_rotated,access_key_1_last_used_date,access_key_1_last_used_region," +
		"access_key_1_last_used_service,access_key_2_active,access_key_2_last_rotated,access_key_2_last_used_date," +
		"access_key_2_last_used_region,access_key_2_last_used_service,cert_1_active,cert_1_last_rotated,cert_2_active,cert_2_last_rotated"
	reportRoot = "<root_account>,arn:aws:iam::123456789012:root,2025-01-01T00:00:00+00:00,not_supported,no_information,not_supported," +
		"not_supported,true,false,N/A,N/A,N/A,N/A,false,N/A,N/A,N/A,N/A,false,N/A,false,N/A"
	reportAlice = "alice,arn:aws:iam::123456789012:user/alice,2025-02-01T00:00:00+00:00,true,2026-01-01T00:00:00+00:00," +
		"2025-03-01T00:00:00+00:00,N/A,false,true,2025-04-01T00:00:00+00:00,2026-01-01T00:00:00+00:00,eu-west-1,s3,false,N/A,N/A,N/A,N/A,false,N/A,false,N/A"
	reportBob = "bob,arn:aws:iam::123456789012:user/bob,2025-02-02T00:00:00+00:00,false,N/A,N/A,N/A,false,false,N/A,N/A,N/A,N/A,false,N/A,N/A,N/A,N/A,false,N/A,false,N/A"
)

// reportAnswer is GetCredentialReport with the given CSV text as the base64 blob.
func reportAnswer(csv, generated string) reply {
	return iamAnswer("GetCredentialReport", `<Content>`+base64.StdEncoding.EncodeToString([]byte(csv))+`</Content>`+
		`<ReportFormat>text/csv</ReportFormat><GeneratedTime>`+generated+`</GeneratedTime>`)
}

func csvOf(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

func reportEnv(t *testing.T, answer func(sent, int) reply) (*env, *fakeAWS) {
	t.Helper()
	f := direct(answer)
	return newToolsEnv(t, f, baseValues(), []string{"aws.iam.credential-report.generate", "aws.iam.credential-report.get",
		"aws.iam.service-last-accessed.generate", "aws.iam.service-last-accessed.get-details",
		"aws.iam.service-last-accessed.get-entities", "aws.iam.service-access.list-policies"}), f
}

// The generators are listed-only executes with confirmation that are not repeated; the readers are open
// safe reads.
func TestIAMReportToolsRisk(t *testing.T) {
	for _, id := range []string{"aws.iam.credential-report.generate", "aws.iam.service-last-accessed.generate"} {
		d := specOf(t, id).descriptor
		if !d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationRequired || d.Risk.Effect != capability.EffectExecute ||
			d.Risk.Idempotency != capability.IdempotencyNonIdempotent || !d.Risk.OpenWorld {
			t.Errorf("%s descriptor = %+v", id, d)
		}
		if !strings.Contains(d.Description, "Free of charge") || !strings.Contains(d.Description, "changes no account data") {
			t.Errorf("%s description does not say that it is free and changes nothing", id)
		}
	}
	for _, id := range []string{"aws.iam.credential-report.get", "aws.iam.service-last-accessed.get-details",
		"aws.iam.service-last-accessed.get-entities", "aws.iam.service-access.list-policies"} {
		d := specOf(t, id).descriptor
		if d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationNone || d.Risk.Effect != capability.EffectRead ||
			d.Risk.Idempotency != capability.IdempotencySafe || !d.Risk.OpenWorld {
			t.Errorf("%s descriptor = %+v", id, d)
		}
	}
}

// Generate returns the state AWS reports in each of its three states, and needs confirmation.
func TestIAMGenerateCredentialReportStates(t *testing.T) {
	for _, state := range []string{"STARTED", "INPROGRESS", "COMPLETE"} {
		e, f := reportEnv(t, func(sent, int) reply {
			return iamAnswer("GenerateCredentialReport", `<State>`+state+`</State><Description>text of AWS</Description>`)
		})
		text, err := e.invokeConfirmed("aws.iam.credential-report.generate", `{}`, true)
		if err != nil {
			t.Fatalf("%s: %v", state, err)
		}
		got := decodeResult(t, text)
		if got["state"] != state || got["description"] != "text of AWS" || f.count("GenerateCredentialReport") != 1 {
			t.Errorf("%s: result = %v, requests = %v", state, got, f.actions())
		}
	}
	e, f := reportEnv(t, nil)
	var unconfirmed *application.ConfirmationRequiredError
	if _, err := e.invoke("aws.iam.credential-report.generate", `{}`); !errorsAs(err, &unconfirmed) || len(f.requests) != 0 {
		t.Errorf("unconfirmed: %v after %v", err, f.actions())
	}
}

// The credential report becomes rows: known columns under the names of the report, booleans as booleans,
// empty, N/A and not_supported fields left out, unknown columns dropped and named, and the raw blob never
// returned.
func TestIAMCredentialReportRows(t *testing.T) {
	csv := csvOf(reportHeader+",secret_column,evil column", reportRoot+",x,y", reportAlice+",x,y")
	e, f := reportEnv(t, func(sent, int) reply { return reportAnswer(csv, generatedAt) })
	text, err := e.invoke("aws.iam.credential-report.get", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeResult(t, text)
	root, alice := resultAt(got, "rows.0").(map[string]any), resultAt(got, "rows.1").(map[string]any)
	if root["user"] != "<root_account>" || root["mfa_active"] != true || root["access_key_1_active"] != false {
		t.Errorf("root row = %v", root)
	}
	for _, key := range []string{"password_enabled", "password_last_changed", "password_next_rotation", "access_key_1_last_rotated", "access_key_2_last_used_date"} {
		if _, has := root[key]; has {
			t.Errorf("root row keeps the absent field %s: %v", key, root)
		}
	}
	if root["password_last_used"] != "no_information" {
		t.Errorf("password_last_used = %v, want the text of the report", root["password_last_used"])
	}
	if alice["password_enabled"] != true || alice["access_key_1_last_used_service"] != "s3" || alice["access_key_1_last_used_region"] != "eu-west-1" {
		t.Errorf("alice row = %v", alice)
	}
	if fmt.Sprint(got["dropped_columns"]) != "[secret_column]" || got["total_rows"] != float64(2) || got["has_more"] != false ||
		got["generated_time"] != generatedAt || got["report_format"] != "text/csv" {
		t.Errorf("result = %v", got)
	}
	for _, leak := range []string{base64.StdEncoding.EncodeToString([]byte(csv)), "content", "\"x\"", "evil"} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(leak)) {
			t.Errorf("result holds %q: %s", leak, text)
		}
	}
	if f.count("GetCredentialReport") != 1 {
		t.Errorf("requests = %v", f.actions())
	}
}

// The rows page over three pages by a cursor bound to the tool and the report: a tampered cursor, a cursor
// of another report generation, and a bad page size are refused without a request to AWS.
func TestIAMCredentialReportPages(t *testing.T) {
	generation := generatedAt
	csv := csvOf(reportHeader, reportRoot, reportAlice, reportBob)
	e, f := reportEnv(t, func(sent, int) reply { return reportAnswer(csv, generation) })
	var users []string
	cursor := ""
	for page := 0; page < 3; page++ {
		in := `{"page_size":1}`
		if cursor != "" {
			in = `{"page_size":1,"cursor":"` + cursor + `"}`
		}
		text, err := e.invoke("aws.iam.credential-report.get", in)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		got := decodeResult(t, text)
		users = append(users, fmt.Sprint(resultAt(got, "rows.0.user")))
		next, _ := got["next_cursor"].(string)
		if len(got["rows"].([]any)) != 1 || got["has_more"] != (page < 2) || (next != "") != (page < 2) || got["total_rows"] != float64(3) {
			t.Errorf("page %d: %v", page, got)
		}
		cursor = next
	}
	if fmt.Sprint(users) != "[<root_account> alice bob]" {
		t.Errorf("users = %v", users)
	}
	text, _ := e.invoke("aws.iam.credential-report.get", `{"page_size":2}`)
	issued, _ := decodeResult(t, text)["next_cursor"].(string)
	before := len(f.requests)
	tampered := issued[:len(issued)-1] + "x"
	if tampered == issued {
		tampered = issued[:len(issued)-1] + "y"
	}
	for name, in := range map[string]string{
		"tampered":      `{"cursor":"` + tampered + `"}`,
		"garbage":       `{"cursor":"!!"}`,
		"page size 0":   `{"page_size":0}`,
		"page size big": `{"page_size":501}`,
	} {
		if _, err := e.invoke("aws.iam.credential-report.get", in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if len(f.requests) != before {
		t.Errorf("a refused page was sent: %v", f.actions()[before:])
	}
	// The report was generated again between two pages: the cursor no longer points into it.
	generation = "2026-02-02T03:04:05Z"
	if _, err := e.invoke("aws.iam.credential-report.get", `{"cursor":"`+issued+`"}`); !isInvalidRequest(err) {
		t.Errorf("a cursor of an older report: %v", err)
	}
	// A cursor of another tool does not continue this one.
	other, _ := reportEnv(t, func(sent, int) reply {
		return iamPage("GetServiceLastAccessedDetails", "ServicesLastAccessed", jobExtra, "", "")
	})
	if _, err := other.invoke("aws.iam.credential-report.get", `{"cursor":"`+issued+`"}`); err == nil {
		t.Error("a cursor passed a binding it was not issued for")
	}
}

// A report that cannot be read as the known CSV is an invalid answer, never a guess.
func TestIAMCredentialReportInvalid(t *testing.T) {
	long := strings.Repeat("a", 2049)
	cases := map[string]reply{
		"not base64":        iamAnswer("GetCredentialReport", `<Content>***</Content><ReportFormat>text/csv</ReportFormat><GeneratedTime>`+generatedAt+`</GeneratedTime>`),
		"not utf-8":         reportAnswer("user,arn\n\xff,arn:x\n", generatedAt),
		"empty":             reportAnswer("", generatedAt),
		"no required":       reportAnswer(csvOf("user,mfa_active", "alice,true"), generatedAt),
		"duplicate header":  reportAnswer(csvOf("user,arn,user", "a,b,c"), generatedAt),
		"ragged row":        reportAnswer(csvOf("user,arn", "alice"), generatedAt),
		"bare quote":        reportAnswer(csvOf("user,arn", `al"ice,arn:x`), generatedAt),
		"row without user":  reportAnswer(csvOf("user,arn", ",arn:x"), generatedAt),
		"row without arn":   reportAnswer(csvOf("user,arn", "alice,N/A"), generatedAt),
		"bad boolean":       reportAnswer(csvOf("user,arn,mfa_active", "alice,arn:x,maybe"), generatedAt),
		"field too long":    reportAnswer(csvOf("user,arn", "alice,"+long), generatedAt),
		"too many rows":     reportAnswer("user,arn\n"+strings.Repeat("a,b\n", 10001), generatedAt),
		"too many columns":  reportAnswer(csvOf("user,arn,"+strings.TrimSuffix(strings.Repeat("c,", 70), ",")), generatedAt),
		"without content":   iamAnswer("GetCredentialReport", `<ReportFormat>text/csv</ReportFormat><GeneratedTime>`+generatedAt+`</GeneratedTime>`),
		"without generated": iamAnswer("GetCredentialReport", `<Content>dXNlcixhcm4K</Content><ReportFormat>text/csv</ReportFormat>`),
	}
	for name, answer := range cases {
		e, _ := reportEnv(t, func(sent, int) reply { return answer })
		if _, err := e.invoke("aws.iam.credential-report.get", `{}`); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: err = %v, want invalid-provider-response", name, err)
		}
	}
	// A header-only report is an empty report, not an error.
	e, _ := reportEnv(t, func(sent, int) reply { return reportAnswer(csvOf("user,arn"), generatedAt) })
	text, err := e.invoke("aws.iam.credential-report.get", `{}`)
	if got := decodeResult(t, text); err != nil || got["total_rows"] != float64(0) || got["has_more"] != false {
		t.Errorf("empty report: %v, %v", got, err)
	}
}

// The error codes of the report map to the classes of the contract, with a message that is not AWS's text.
func TestIAMReportErrors(t *testing.T) {
	for code, c := range map[string]struct {
		status int
		class  provider.Class
		text   string
	}{
		"ReportNotPresent": {410, provider.ClassNotFound, "generate one"},
		"ReportInProgress": {404, provider.ClassProviderError, "still generating"},
		"ReportExpired":    {410, provider.ClassProviderError, "expired"},
		"Throttling":       {400, provider.ClassRateLimited, "rate-limited"},
	} {
		e, f := reportEnv(t, func(sent, int) reply { return reply{status: c.status, body: iamErrorFor(code)} })
		_, err := e.invoke("aws.iam.credential-report.get", `{}`)
		var failed *provider.Error
		if !errorsAs(err, &failed) || failed.Class != c.class || !strings.Contains(failed.Message, c.text) ||
			strings.Contains(failed.Message, "<") || f.count("GetCredentialReport") != 1 {
			t.Errorf("%s: err = %v after %d requests", code, err, f.count("GetCredentialReport"))
		}
	}
	// A generator that is throttled is not repeated.
	for _, id := range []string{"aws.iam.credential-report.generate", "aws.iam.service-last-accessed.generate"} {
		e, f := reportEnv(t, func(sent, int) reply { return reply{status: 400, body: iamErrorFor("Throttling")} })
		args := `{}`
		if id == "aws.iam.service-last-accessed.generate" {
			args = `{"arn":"arn:aws:iam::123456789012:role/r"}`
		}
		_, err := e.invokeConfirmed(id, args, true)
		n := f.count("GenerateCredentialReport") + f.count("GenerateServiceLastAccessedDetails")
		if classOf(err) != provider.ClassRateLimited || n != 1 {
			t.Errorf("%s: err = %v, %d requests", id, err, n)
		}
	}
}

// The ARN of a report job belongs to the bound account: another account, an AWS managed policy, and another
// partition are refused before any request, and the refusal does not repeat the value.
func TestIAMReportARNBinding(t *testing.T) {
	for _, c := range []struct{ tool, arg string }{
		{"aws.iam.service-last-accessed.generate", `{"arn":"%s"}`},
		{"aws.iam.service-access.list-policies", `{"arn":"%s","service_namespaces":["s3"]}`},
	} {
		for _, arn := range []string{"arn:aws:iam::210987654321:role/r", "arn:aws:iam::aws:policy/ReadOnlyAccess",
			"arn:aws-cn:iam::123456789012:role/r", "role/r"} {
			e, f := reportEnv(t, nil)
			_, err := e.invokeConfirmed(c.tool, fmt.Sprintf(c.arg, arn), true)
			if !isInvalidRequest(err) || len(f.requests) != 0 || strings.Contains(err.Error(), arn) {
				t.Errorf("%s %s: err = %v, requests = %v", c.tool, arn, err, f.actions())
			}
		}
	}
	e, f := reportEnv(t, func(r sent, _ int) reply {
		return iamAnswer("GenerateServiceLastAccessedDetails", `<JobId>`+jobID+`</JobId>`)
	})
	text, err := e.invokeConfirmed("aws.iam.service-last-accessed.generate",
		`{"arn":"arn:aws:iam::123456789012:policy/deploy","granularity":"ACTION_LEVEL"}`, true)
	var call sent
	for _, r := range f.requests {
		if r.action == "GenerateServiceLastAccessedDetails" {
			call = r
		}
	}
	if err != nil || decodeResult(t, text)["job_id"] != jobID || call.form.Get("Granularity") != "ACTION_LEVEL" ||
		call.form.Get("Arn") != "arn:aws:iam::123456789012:policy/deploy" {
		t.Errorf("generate: %v, %v, form %v", text, err, call.form)
	}
	var unconfirmed *application.ConfirmationRequiredError
	before := len(f.requests)
	if _, err := e.invoke("aws.iam.service-last-accessed.generate", `{"arn":"arn:aws:iam::123456789012:role/r"}`); !errorsAs(err, &unconfirmed) || len(f.requests) != before {
		t.Errorf("unconfirmed: %v", err)
	}
}

// The policies that grant service access page by Marker over three pages without a page size, and the
// service namespaces reach AWS as a list.
func TestIAMListPoliciesGrantingServiceAccess(t *testing.T) {
	entry := `<member><ServiceNamespace>s3</ServiceNamespace><Policies><member><PolicyName>p</PolicyName><PolicyType>INLINE</PolicyType>` +
		`<EntityType>ROLE</EntityType><EntityName>r</EntityName></member></Policies></member>`
	e, f := reportEnv(t, func(r sent, _ int) reply {
		switch r.form.Get("Marker") {
		case "":
			return iamPage("ListPoliciesGrantingServiceAccess", "PoliciesGrantingServiceAccess", "", entry, "m1")
		case "m1":
			return iamPage("ListPoliciesGrantingServiceAccess", "PoliciesGrantingServiceAccess", "", entry, "m2")
		}
		return iamPage("ListPoliciesGrantingServiceAccess", "PoliciesGrantingServiceAccess", "", entry, "")
	})
	args := `"arn":"arn:aws:iam::123456789012:role/r","service_namespaces":["s3","ec2"]`
	cursor, first := "", ""
	for page := 0; page < 3; page++ {
		in := `{` + args + `}`
		if cursor != "" {
			in = `{` + args + `,"cursor":"` + cursor + `"}`
		}
		text, err := e.invoke("aws.iam.service-access.list-policies", in)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		got := decodeResult(t, text)
		next, _ := got["next_cursor"].(string)
		if got["has_more"] != (page < 2) || (next != "") != (page < 2) || resultAt(got, "policies_granting_service_access.0.service_namespace") != "s3" {
			t.Errorf("page %d: %v", page, got)
		}
		if page == 0 {
			first = next
		}
		cursor = next
	}
	last := f.requests[len(f.requests)-1].form
	if last.Get("ServiceNamespaces.member.1") != "s3" || last.Get("ServiceNamespaces.member.2") != "ec2" || last.Get("Marker") != "m2" {
		t.Errorf("form = %v", last)
	}
	if _, err := e.invoke("aws.iam.service-access.list-policies", `{`+args+`,"page_size":5}`); !isInvalidRequest(err) {
		t.Errorf("page_size is not an argument of this tool: %v", err)
	}
	if _, err := e.invoke("aws.iam.service-access.list-policies", `{`+args+`,"cursor":"`+first[:len(first)-1]+`x"}`); err == nil {
		t.Error("a tampered cursor was accepted")
	}
}
