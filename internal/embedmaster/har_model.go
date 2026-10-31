// Package embedmaster models the HAR (HTTP Archive) v1.2 file recorded at
// embedmaster.com. It is a structural model only: any network action (HTTP,
// curl, wget, ...) is deliberately absent; playback/decoding of recorded
// responses, including the parts that require running JavaScript, is the
// caller's business (the harness shells out to `node -e`).
package embedmaster

// HAR is the root object of a HAR file.
type HAR struct {
	Log Log `json:"log"`
}

// Log holds the creator metadata and the recorded entries.
type Log struct {
	Version string  `json:"version"`
	Creator Creator `json:"creator"`
	Entries []Entry `json:"entries"`
}

// Creator describes the tool that produced the HAR.
type Creator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Entry is one recorded request/response pair.
type Entry struct {
	StartedDateTime string   `json:"startedDateTime"`
	Time            float64  `json:"time"`
	Request         Request  `json:"request"`
	Response        Response `json:"response"`
}

// Request is the recorded request of an entry.
type Request struct {
	Method      string    `json:"method"`
	URL         string    `json:"url"`
	HTTPVersion string    `json:"httpVersion"`
	Headers     []Header  `json:"headers"`
	QueryString []Param   `json:"queryString"`
	PostData    *PostData `json:"postData,omitempty"`
	HeaderSize  int64     `json:"headersSize"`
	BodySize    int64     `json:"bodySize"`
}

// Response is the recorded response of an entry.
type Response struct {
	Status      int      `json:"status"`
	StatusText  string   `json:"statusText"`
	HTTPVersion string   `json:"httpVersion"`
	Headers     []Header `json:"headers"`
	Content     Content  `json:"content"`
	RedirectURL string   `json:"redirectURL"`
	HeaderSize  int64    `json:"headersSize"`
	BodySize    int64    `json:"bodySize"`
}

// Content is the (possibly truncated / base64 encoded) response body.
type Content struct {
	Size        int64  `json:"size"`
	Compression int64  `json:"compression,omitempty"`
	MimeType    string `json:"mimeType"`
	Text        string `json:"text,omitempty"`
	Encoding    string `json:"encoding,omitempty"`
}

// Header is a single HTTP header name/value pair.
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Param is a query param or a POST form parameter.
type Param struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Comment string `json:"comment,omitempty"`
}

// PostData is the recorded request body of an entry.
type PostData struct {
	MimeType string  `json:"mimeType"`
	Params   []Param `json:"params,omitempty"`
	Text     string  `json:"text,omitempty"`
}
