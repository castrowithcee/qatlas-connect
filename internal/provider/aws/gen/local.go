package gen

import "github.com/castrowithcee/qatlas-connect/internal/provider/aws/httpbind"

// The tools with Operation.LocalFile move their payload through a local file. Everything about that file is
// the runtime's: the table only says in which direction.

const (
	localPathName = "local_path"
	// localPathSchema is localfile.LocalPathSchema; the generator does not import the runtime packages that
	// open files.
	localPathSchema = `{"type":"string","minLength":1,"maxLength":4096}`
)

// localPathHelp is the description of local_path, by direction.
var localPathHelp = map[string]string{
	"read": "Local file to upload, at most 100 MiB, absolute or starting with ~/, inside a directory the connection " +
		"releases for reading; its own name is not sent",
	// A range tool reads one part of a file that may be larger than a call sends.
	"read-range": "Local file to read the part from, absolute or starting with ~/, inside a directory the connection " +
		"releases for reading; the file may be larger than the part, its own name is not sent",
	"write": "Local file to write, absolute or starting with ~/, in an existing directory the connection releases for " +
		"writing; an existing file is replaced only with confirmation",
}

// The members offset and length of a tool with Operation.LocalRange select the part of the file that is sent.
const (
	localOffsetName = "offset"
	localLengthName = "length"
	// localRangeMax is httpbind.MaxStreamBytes, the most one call sends.
	localRangeMax = 100 << 20
)

const (
	localOffsetHelp = "Byte position in the local file where the range starts; 0 (the start) when left out"
	localLengthHelp = "Number of bytes of the range, 1 to 104857600 (100 MiB); up to the end of the file when left out, which then " +
		"must be at most 100 MiB after offset"
)

// localResult are the result members the runtime adds for the file, by direction of the description.
var localResult = []struct {
	name        string
	schema      *object
	description map[string]string
}{{
	name:   localPathName,
	schema: newObject("type", "string", "maxLength", 4096),
	description: map[string]string{
		"read":  "Local file that was uploaded, as given",
		"write": "Local file that was written, as given",
	},
}, {
	name:   "size",
	schema: newObject("type", "integer", "minimum", 0),
	description: map[string]string{
		"read":  "Number of bytes uploaded",
		"write": "Number of bytes written to the file",
	},
}, {
	name:   "sha256",
	schema: newObject("type", "string", "pattern", "^[0-9a-f]{64}$"),
	description: map[string]string{
		"read":  "SHA-256 of the uploaded content as hex, which S3 verified against x-amz-checksum-sha256",
		"write": "SHA-256 of the written file as hex; compared with the checksum S3 states, when it states one",
	},
}}

// isStream reports whether the top-level member name of the input (or the output) is the streamed payload
// that local_path carries.
func (t *tool) isStream(name string, input bool) bool {
	if t.op.LocalFile == "" || (t.op.LocalFile == "read") != input {
		return false
	}
	shape := t.restBind.Operation.Output
	if input {
		shape = t.restBind.Operation.Input
	}
	f := fieldByKey(shape, name)
	return f != nil && f.Stream
}

// checkLocalFile requires the model to have exactly the stream the direction needs, and none the other way.
func (t *tool) checkLocalFile() error {
	if !t.rest || !t.xml {
		return t.failf("local_file belongs to a restXml tool")
	}
	has := func(s *httpbind.Shape) int {
		n := 0
		if s != nil {
			for _, f := range s.Fields {
				if f.Stream {
					n++
				}
			}
		}
		return n
	}
	in, out := has(t.restBind.Operation.Input), has(t.restBind.Operation.Output)
	if (t.op.LocalFile == "read" && (in != 1 || out != 0)) || (t.op.LocalFile == "write" && (out != 1 || in != 0)) {
		return t.failf("the model has %d streamed input and %d streamed output members, which does not fit local_file %s", in, out, t.op.LocalFile)
	}
	return nil
}
