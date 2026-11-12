package aws

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	logZone  = "Z0123456789ABCDEFGHIJ"
	logGroup = "arn:aws:logs:us-east-1:" + testAccount + ":log-group:/aws/route53/example.com"
)

const loggingXML = `<QueryLoggingConfig><Id>cfg-1</Id><HostedZoneId>` + logZone + `</HostedZoneId><CloudWatchLogsLogGroupArn>` + logGroup + `</CloudWatchLogsLogGroupArn></QueryLoggingConfig>`

func logCreateArgs(arn string) string {
	return `{"hosted_zone_id":"Z1","cloud_watch_logs_log_group_arn":"` + arn + `"}`
}

// Create and delete send their request in the shape of the model.
func TestRoute53QueryLoggingWritesSendTheirRequest(t *testing.T) {
	f := direct(func(r sent, _ int) reply {
		if r.method == "POST" {
			rep := xmlReply(`<CreateQueryLoggingConfigResponse ` + route53NS + `>` + loggingXML + `</CreateQueryLoggingConfigResponse>`)
			rep.status, rep.header["Location"] = 201, "https://route53.amazonaws.com/2013-04-01/queryloggingconfig/cfg-1"
			return rep
		}
		return xmlReply(`<DeleteQueryLoggingConfigResponse ` + route53NS + `/>`)
	})
	e := route53Env(t, f, "us-east-1")
	// The zone ID may carry its prefix; the body carries the bare ID.
	text, err := e.invokeConfirmed("aws.route53.query-logging-config.create", strings.Replace(logCreateArgs(logGroup), `"Z1"`, `"/hostedzone/Z1"`, 1), true)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := decodeResult(t, text)["query_logging_config"].(map[string]any)
	if cfg["id"] != "cfg-1" || cfg["cloud_watch_logs_log_group_arn"] != logGroup {
		t.Errorf("create result = %s", text)
	}
	if _, err := e.invokeConfirmed("aws.route53.query-logging-config.delete", `{"id":"cfg-1"}`, true); err != nil {
		t.Fatal(err)
	}
	reqs := restRequests(f)
	if r := reqs[0]; r.method != "POST" || r.path != "/2013-04-01/queryloggingconfig" ||
		!strings.Contains(r.body, "<HostedZoneId>Z1</HostedZoneId>") ||
		!strings.Contains(r.body, "<CloudWatchLogsLogGroupArn>"+logGroup+"</CloudWatchLogsLogGroupArn>") {
		t.Errorf("create request = %+v", r)
	}
	if r := reqs[1]; r.method != "DELETE" || r.path != "/2013-04-01/queryloggingconfig/cfg-1" {
		t.Errorf("delete request = %+v", r)
	}
}

// The log group must be a log group of the bound account in us-east-1 that the connection allows; anything else is
// refused before any request.
func TestRoute53QueryLoggingChecksTheLogGroupBeforeAnyRequest(t *testing.T) {
	const name = ":log-group:/aws/route53/example.com"
	f := restAnswer(reply{status: 500})
	e := route53Env(t, f, "us-east-1", "eu-central-1")
	for _, arn := range []string{
		"arn:aws:logs:us-east-1:999999999999" + name,                     // another account
		"arn:aws:logs:eu-central-1:" + testAccount + name,                // another region, allowed by the connection
		"arn:aws:logs:eu-west-1:" + testAccount + name,                   // another region, not allowed
		"arn:aws:logs:us-east-1:" + testAccount + ":log-group:*",         // wildcard
		"arn:aws:logs:us-east-1:" + testAccount + ":log-group:/aws/*",    // wildcard in the name
		"arn:aws:logs:us-east-1:" + testAccount + name + ":*",            // console style suffix
		"arn:aws:logs:us-east-1:" + testAccount + name + ":log-stream:s", // log stream
		"arn:aws:logs:us-east-1:" + testAccount + ":destination:d",       // not a log group
		"arn:aws:logs:us-east-1:" + testAccount + ":log-group:",          // empty name
		"arn:aws:s3:::bucket", "arn:aws:sns:us-east-1:" + testAccount + ":log-group:a",
		"arn:aws-cn:logs:us-east-1:" + testAccount + name, "arn:aws:logs:us-east-1::log-group:a",
		"/aws/route53/example.com", "arn:aws:logs", "",
	} {
		if _, err := e.invokeConfirmed("aws.route53.query-logging-config.create", logCreateArgs(arn), true); err == nil {
			t.Errorf("%q was accepted", arn)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused call reached the network: %v", f.actions())
	}
	// A connection without us-east-1 in its allowlist cannot log there.
	noEast := route53Env(t, f, "eu-central-1")
	if _, err := noEast.invokeConfirmed("aws.route53.query-logging-config.create", logCreateArgs(logGroup), true); err == nil {
		t.Error("us-east-1 outside the allowlist was accepted")
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused call reached the network: %v", f.actions())
	}
}

// Errors keep their class and code, and a failed write is not repeated.
func TestRoute53QueryLoggingErrors(t *testing.T) {
	create := logCreateArgs(logGroup)
	del := `{"id":"cfg-1"}`
	for _, c := range []struct {
		id, args, code string
		status         int
		class          provider.Class
	}{
		{"create", create, "NoSuchHostedZone", 404, provider.ClassNotFound},
		{"create", create, "ConcurrentModification", 400, provider.ClassProviderError},
		{"create", create, "QueryLoggingConfigAlreadyExists", 409, provider.ClassProviderError},
		{"create", create, "InsufficientCloudWatchLogsResourcePolicy", 400, provider.ClassProviderError},
		{"create", create, "NoSuchCloudWatchLogsLogGroup", 404, provider.ClassNotFound},
		{"delete", del, "NoSuchQueryLoggingConfig", 404, provider.ClassNotFound},
		{"delete", del, "ConcurrentModification", 400, provider.ClassProviderError},
	} {
		t.Run(c.id+"/"+c.code, func(t *testing.T) {
			f := restAnswer(route53Error(c.status, c.code))
			e := route53Env(t, f, "us-east-1")
			_, err := e.invokeConfirmed("aws.route53.query-logging-config."+c.id, c.args, true)
			var failed *provider.Error
			if !errorsAs(err, &failed) || failed.Class != c.class {
				t.Fatalf("err = %v, want class %s", err, c.class)
			}
			if failed.Original == nil || len(failed.Original.Entries) != 1 || failed.Original.Entries[0].Code != c.code {
				t.Errorf("original = %+v", failed.Original)
			}
			if n := len(restRequests(f)); n != 1 {
				t.Errorf("requests = %d, a failed call was repeated", n)
			}
		})
	}
}

// Both writes need confirmation before they reach the network, and carry the risk the contract names.
func TestRoute53QueryLoggingWritesNeedConfirmation(t *testing.T) {
	f := restAnswer(reply{status: 500})
	e := route53Env(t, f, "us-east-1")
	for id, args := range map[string]string{
		"aws.route53.query-logging-config.create": logCreateArgs(logGroup),
		"aws.route53.query-logging-config.delete": `{"id":"cfg-1"}`,
	} {
		if _, err := e.invoke(id, args); !isConfirmationRequired(err) {
			t.Errorf("%s: err = %v, want a confirmation", id, err)
		}
		d := specOf(t, id).descriptor
		if d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("an unconfirmed write reached the network: %v", f.actions())
	}
	d := specOf(t, "aws.route53.query-logging-config.create").descriptor
	for _, part := range []string{"InsufficientCloudWatchLogsResourcePolicy", "route53.amazonaws.com", "bills"} {
		if !strings.Contains(d.Description, part) {
			t.Errorf("create description lacks %q", part)
		}
	}
}
