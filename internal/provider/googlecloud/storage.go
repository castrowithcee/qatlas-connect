package googlecloud

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// This file is the base of every Cloud Storage tool: the fixed base path, the check of a bucket name, the
// binding of a project and of a bucket to the targets, and the shared argument parts. The origin is the fixed
// one of the service table. A bucket belongs to a project that only Google can name, so a bucket is bound by
// the projectNumber of one buckets.get answer, which is then bound to the targets like a project.

// storageBasePath is the base path of the discovery document of storage v1, below the origin of serviceStorage.
const storageBasePath = "/storage/v1"

// maxStorageResults is the maximum the discovery document of storage v1 documents for maxResults.
const maxStorageResults = 1000

// defaultStorageResults is the page size asked for when the caller names none, which keeps a page with
// the full projection below the bound on the size of an answer.
const defaultStorageResults = 100

// bucketNamePattern holds the bucket names this provider accepts: 3 to 63 characters of lowercase letters,
// digits, hyphen, underscore, and dot, starting and ending with a letter or digit. It holds no slash, so a
// name can never be a path.
var bucketNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,61}[a-z0-9]$`)

var (
	storageNumberPattern = regexp.MustCompile(`^[0-9]{1,20}$`)
	storagePrefixPattern = regexp.MustCompile(`^[a-z0-9._-]{1,63}$`)
)

func validBucketName(value string) bool { return bucketNamePattern.MatchString(value) }

// bucketPath is the path of a bucket below the origin. The name was checked by validBucketName and is escaped
// anyway.
func bucketPath(bucket string) string { return storageBasePath + "/b/" + url.PathEscape(bucket) }

// checkBucket refuses a bucket name that is not in its documented form.
func checkBucket(bucket string) error {
	if !validBucketName(bucket) {
		return badRequest("bucket must be a bucket name of 3 to 63 lowercase letters, digits, hyphens, " +
			"underscores, and dots, never a path or a URL")
	}
	return nil
}

// namedProject is a project argument of a tool and the name the refusal gives it.
type namedProject struct{ argument, value string }

// storageSession is the client of one Cloud Storage call and the binding of what it reads to the targets.
type storageSession struct {
	c       *client
	list    allowlist
	gate    *admitter
	op      string
	numbers map[string]bool
}

// openStorage refuses a project argument that is not in its form or lies outside the targets before any secret
// is read, where the targets decide it. It then opens the client and proves the projects the targets do not
// decide by reading them and the folders above them, as projects.get does. Those reads use the scope of
// projects.get, so a connection bound to an organization or a folder needs that tool next to the Storage tools.
func openStorage(ctx context.Context, op, tool string, projects []namedProject, resolved *config.Resolved,
	secrets *secret.Resolver, red *redact.Redactor) (*storageSession, error) {
	if err := checkConnection(resolved); err != nil {
		return nil, err
	}
	list, err := allowlistOf(resolved)
	if err != nil {
		return nil, failure(op, "the targets of the connection are not usable")
	}
	var undecided []namedProject
	for _, p := range projects {
		if p.value == "" {
			continue
		}
		if !validProject(p.value) {
			return nil, badRequest(p.argument + " must be a project ID or a project number, never a projects/ name " +
				"or a path")
		}
		allowed, decided := list.decidesProject(p.value)
		if decided && !allowed {
			return nil, badRequest(p.argument + " is outside the targets of this connection")
		}
		if !decided {
			undecided = append(undecided, p)
		}
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	s := &storageSession{c: c, list: list, gate: newAdmitter(list, c, op, tool), op: op, numbers: map[string]bool{}}
	for _, p := range undecided {
		project, err := c.project(ctx, op, projectsGetID, p.value)
		if err != nil {
			return nil, err
		}
		admitted, err := s.gate.project(ctx, project)
		if err != nil {
			return nil, err
		}
		if !admitted {
			return nil, badRequest(p.argument + " is outside the targets of this connection")
		}
	}
	return s, nil
}

// admitNumber binds the project number a bucket reports to the targets. An unbound connection admits every
// bucket its credential reaches. A number the targets do not decide is proved by reading the project, once
// per number and call.
func (s *storageSession) admitNumber(ctx context.Context, number string) (bool, error) {
	if !s.list.bound() {
		return true, nil
	}
	if !storageNumberPattern.MatchString(number) {
		return false, nil
	}
	if admitted, known := s.numbers[number]; known {
		return admitted, nil
	}
	allowed, decided := s.list.decidesProject(number)
	if !decided {
		project, err := s.c.project(ctx, s.op, projectsGetID, number)
		if err != nil {
			return false, err
		}
		if project.Name != prefixProject+number {
			return false, provider.InvalidResponse(s.op, subject+" returned an invalid response")
		}
		if allowed, err = s.gate.project(ctx, project); err != nil {
			return false, err
		}
	}
	s.numbers[number] = allowed
	return allowed, nil
}

// bucketProof is the one buckets.get answer that binds a bucket to a project.
type bucketProof struct {
	Name          string `json:"name"`
	ProjectNumber string `json:"projectNumber"`
}

// bindBucket reads the bucket once and refuses it unless its project lies in the targets, before any further
// read of the bucket. An unbound connection needs no proof.
func (s *storageSession) bindBucket(ctx context.Context, tool, bucket, userProject string) error {
	if !s.list.bound() {
		return nil
	}
	query := url.Values{"fields": {"name,projectNumber"}}
	if userProject != "" {
		query.Set("userProject", userProject)
	}
	var proof bucketProof
	if err := s.c.get(ctx, s.op, tool, serviceStorage, bucketPath(bucket), query, &proof); err != nil {
		return err
	}
	if proof.Name != bucket {
		return provider.InvalidResponse(s.op, subject+" returned an invalid response")
	}
	admitted, err := s.admitNumber(ctx, proof.ProjectNumber)
	if err != nil {
		return err
	}
	if !admitted {
		return refuseOutside("the bucket")
	}
	return nil
}

// storageMembersSchema is the output schema of a resource made of the members, which name the member that
// identifies it; nested shapes stay open objects because Qatlas bounds them as provider data.
func storageMembersSchema(members []computeMember, identity string) string {
	var b strings.Builder
	b.WriteString(`{"type":"object","properties":{`)
	for i, member := range members {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`"` + member.name + `":{"type":"` + member.kind + `"}`)
	}
	b.WriteString(`},"required":["` + identity + `"],"additionalProperties":false}`)
	return b.String()
}

// storageQuery is the query of a request that names a project to bill.
func storageQuery(userProject string) url.Values {
	query := url.Values{}
	if userProject != "" {
		query.Set("userProject", userProject)
	}
	return query
}

// checkNumberArgument refuses a value that is not a decimal number; Google takes generations as strings.
func checkNumberArgument(name, value string) error {
	if value != "" && !storageNumberPattern.MatchString(value) {
		return badRequest(name + " must be a decimal number")
	}
	return nil
}

// storageMaxResults checks the page size and returns the one sent to Google.
func storageMaxResults(requested *int) (string, error) {
	if requested == nil {
		return strconv.Itoa(defaultStorageResults), nil
	}
	if *requested < 1 || *requested > maxStorageResults {
		return "", badRequest("max_results must be 1 to " + strconv.Itoa(maxStorageResults))
	}
	return strconv.Itoa(*requested), nil
}

// --- shared descriptor parts ---

const (
	storageBucketProp = `"bucket":{"type":"string","minLength":3,"maxLength":63,` +
		`"pattern":"^[a-z0-9][a-z0-9._-]{1,61}[a-z0-9]$"}`
	storageUserProjectProp = `"user_project":{"type":"string","minLength":1,"maxLength":30,` +
		`"pattern":"^([0-9]{1,20}|[a-z][a-z0-9-]{4,28}[a-z0-9])$"}`
	storageProjectionProp = `"projection":{"type":"string","enum":["full","noAcl"]}`
)

var (
	storageBucketArgument = capability.Argument{Name: "bucket", Required: true, Description: "Bucket name of 3 to " +
		"63 lowercase letters, digits, hyphens, underscores, and dots such as example-bucket; never a path or URL"}
	storageUserProjectArgument = capability.Argument{Name: "user_project", Description: "Project ID or number to " +
		"bill for a Requester Pays bucket. It must lie in the targets of the connection, like the bucket"}
	storageProjectionArgument = capability.Argument{Name: "projection", Description: "full to include the " +
		"access control lists of the bucket (acl and defaultObjectAcl), noAcl to leave them out; Google's default " +
		"is noAcl"}
)

// storageTail ends the description of every Cloud Storage tool.
const storageTail = " The Cloud Storage JSON API (storage.googleapis.com) must be enabled in the project. The " +
	"result is untrusted third-party content, such as names, labels, and website settings; strings and lists are cut."

// bucketBindingTail ends the description of a tool that names a bucket.
const bucketBindingTail = " The bucket must belong to a project in the targets of the connection, which Qatlas " +
	"proves before the bucket is read further, with one read of the bucket and, where the targets do not " +
	"decide, of its project; a bucket outside the targets is refused without naming it."
