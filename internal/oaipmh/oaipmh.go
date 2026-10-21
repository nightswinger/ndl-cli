// Package oaipmh implements the NDL Search OAI-PMH 2.0 harvesting API.
//
// Spec: 外部提供インタフェース仕様書 第1.5版 §6. ListRecords and
// ListIdentifiers require from (range at most one year), return 200 items
// per page and continue via resumptionToken.
package oaipmh

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/nightswinger/ndl-cli/internal/client"
	"github.com/nightswinger/ndl-cli/internal/dcndl"
)

// Path is the OAI-PMH endpoint relative to the API base URL.
const Path = "/oaipmh"

// Verbs.
const (
	VerbIdentify            = "Identify"
	VerbListMetadataFormats = "ListMetadataFormats"
	VerbListSets            = "ListSets"
	VerbListIdentifiers     = "ListIdentifiers"
	VerbListRecords         = "ListRecords"
	VerbGetRecord           = "GetRecord"
)

// Prefixes are the metadataPrefix values the server supports.
var Prefixes = []string{"dcndl", "dcndl_v3", "oai_dc"}

// Request holds OAI-PMH arguments. ResumptionToken is exclusive: when set,
// only verb and token are sent.
type Request struct {
	Verb            string
	Identifier      string
	MetadataPrefix  string
	From            string
	Until           string
	Set             string
	ResumptionToken string
}

// Values encodes the request.
func (r Request) Values() url.Values {
	v := url.Values{}
	v.Set("verb", r.Verb)
	if r.ResumptionToken != "" {
		v.Set("resumptionToken", r.ResumptionToken)
		return v
	}
	set := func(k, s string) {
		if s != "" {
			v.Set(k, s)
		}
	}
	switch r.Verb {
	case VerbGetRecord:
		set("identifier", r.Identifier)
		set("metadataPrefix", r.MetadataPrefix)
	case VerbListMetadataFormats:
		set("identifier", r.Identifier)
	case VerbListIdentifiers, VerbListRecords:
		set("metadataPrefix", r.MetadataPrefix)
		set("from", r.From)
		set("until", r.Until)
		set("set", r.Set)
	}
	return v
}

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(T\d{2}:\d{2}:\d{2}Z)?$`)

// Validate checks the arguments the server requires.
func (r Request) Validate() error {
	if r.ResumptionToken != "" {
		return nil
	}
	checkPrefix := func() error {
		if r.MetadataPrefix == "" {
			return errors.New("metadataPrefix is required")
		}
		for _, p := range Prefixes {
			if p == r.MetadataPrefix {
				return nil
			}
		}
		return fmt.Errorf("metadataPrefix must be one of %s", strings.Join(Prefixes, ", "))
	}
	switch r.Verb {
	case VerbGetRecord:
		if r.Identifier == "" {
			return errors.New("identifier is required")
		}
		return checkPrefix()
	case VerbListIdentifiers, VerbListRecords:
		if err := checkPrefix(); err != nil {
			return err
		}
		if r.From == "" {
			return errors.New("--from is required for ListRecords / ListIdentifiers (NDL Search access restriction; range is at most 1 year)")
		}
		for _, d := range []string{r.From, r.Until} {
			if d != "" && !dateRe.MatchString(d) {
				return fmt.Errorf("date must be YYYY-MM-DD or YYYY-MM-DDThh:mm:ssZ: %q", d)
			}
		}
	}
	return nil
}

// Response is a parsed OAI-PMH response. Only the part matching the verb is set.
type Response struct {
	ResponseDate string           `xml:"responseDate"`
	Errors       []Error          `xml:"error"`
	Identify     *Identify        `xml:"Identify"`
	Formats      []MetadataFormat `xml:"ListMetadataFormats>metadataFormat"`
	Sets         []Set            `xml:"ListSets>set"`
	Headers      []Header         `xml:"ListIdentifiers>header"`
	Records      []Record         `xml:"ListRecords>record"`
	GetRecord    []Record         `xml:"GetRecord>record"`

	SetsToken        *ResumptionToken `xml:"ListSets>resumptionToken"`
	IdentifiersToken *ResumptionToken `xml:"ListIdentifiers>resumptionToken"`
	RecordsToken     *ResumptionToken `xml:"ListRecords>resumptionToken"`
}

// Identify is the repository description.
type Identify struct {
	RepositoryName    string   `xml:"repositoryName" json:"repositoryName"`
	BaseURL           string   `xml:"baseURL" json:"baseURL"`
	ProtocolVersion   string   `xml:"protocolVersion" json:"protocolVersion"`
	AdminEmail        []string `xml:"adminEmail" json:"adminEmail,omitempty"`
	EarliestDatestamp string   `xml:"earliestDatestamp" json:"earliestDatestamp"`
	DeletedRecord     string   `xml:"deletedRecord" json:"deletedRecord"`
	Granularity       string   `xml:"granularity" json:"granularity"`
}

// MetadataFormat is one ListMetadataFormats entry.
type MetadataFormat struct {
	Prefix    string `xml:"metadataPrefix" json:"metadataPrefix"`
	Schema    string `xml:"schema" json:"schema,omitempty"`
	Namespace string `xml:"metadataNamespace" json:"metadataNamespace"`
}

// Set is one ListSets entry.
type Set struct {
	Spec string `xml:"setSpec" json:"setSpec"`
	Name string `xml:"setName" json:"setName"`
}

// Header is a record header.
type Header struct {
	Status     string   `xml:"status,attr"`
	Identifier string   `xml:"identifier"`
	Datestamp  string   `xml:"datestamp"`
	SetSpecs   []string `xml:"setSpec"`
}

// Record is a header plus its still-serialized metadata.
type Record struct {
	Header   Header `xml:"header"`
	Metadata struct {
		Inner string `xml:",innerxml"`
	} `xml:"metadata"`
}

// ResumptionToken continues an incomplete list.
type ResumptionToken struct {
	Value            string `xml:",chardata"`
	CompleteListSize int    `xml:"completeListSize,attr"`
	Cursor           int    `xml:"cursor,attr"`
}

// Error is an OAI-PMH <error code="...">.
type Error struct {
	Code    string `xml:"code,attr"`
	Message string `xml:",chardata"`
}

// ErrorList reports OAI-PMH errors.
type ErrorList []Error

func (e ErrorList) Error() string {
	msgs := make([]string, len(e))
	for i, x := range e {
		msgs[i] = x.Code
		if m := strings.TrimSpace(x.Message); m != "" {
			msgs[i] += ": " + m
		}
	}
	return "OAI-PMH error: " + strings.Join(msgs, "; ")
}

// CodeNoRecordsMatch is the OAI-PMH code for an empty result.
const CodeNoRecordsMatch = "noRecordsMatch"

// Parse decodes an OAI-PMH response body.
func Parse(body []byte) (*Response, error) {
	var resp Response
	if err := xml.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse OAI-PMH response: %w", err)
	}
	return &resp, nil
}

// Err returns OAI-PMH errors other than noRecordsMatch, which is an
// empty result rather than a failure.
func (r *Response) Err() error {
	var errs ErrorList
	for _, e := range r.Errors {
		if e.Code != CodeNoRecordsMatch {
			errs = append(errs, e)
		}
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

// NoRecords reports whether the server answered noRecordsMatch.
func (r *Response) NoRecords() bool {
	for _, e := range r.Errors {
		if e.Code == CodeNoRecordsMatch {
			return true
		}
	}
	return false
}

// Token returns the resumptionToken of whichever list is present.
func (r *Response) Token() *ResumptionToken {
	for _, t := range []*ResumptionToken{r.RecordsToken, r.IdentifiersToken, r.SetsToken} {
		if t != nil {
			return t
		}
	}
	return nil
}

// Len returns the number of list items in the response.
func (r *Response) Len() int {
	return len(r.Records) + len(r.Headers) + len(r.Sets) + len(r.GetRecord)
}

// truncate keeps at most n list items.
func (r *Response) truncate(n int) {
	if len(r.Records) > n {
		r.Records = r.Records[:n]
	}
	if len(r.Headers) > n {
		r.Headers = r.Headers[:n]
	}
	if len(r.Sets) > n {
		r.Sets = r.Sets[:n]
	}
}

// Common converts a header into a metadata-less common record.
func (h Header) Common() dcndl.Record {
	var rec dcndl.Record
	rec.SetURL(dcndl.ItemFromOAIIdentifier(h.Identifier))
	rec.OAIIdentifier = strings.TrimSpace(h.Identifier)
	rec.Datestamp = strings.TrimSpace(h.Datestamp)
	rec.Sets = h.SetSpecs
	rec.Deleted = h.Status == "deleted"
	return rec
}

// Common converts the record into the common record type.
func (r Record) Common() (dcndl.Record, error) {
	var rec dcndl.Record
	if md := strings.TrimSpace(r.Metadata.Inner); md != "" {
		var err error
		if rec, err = dcndl.Parse([]byte(md)); err != nil {
			return rec, fmt.Errorf("%s: %w", r.Header.Identifier, err)
		}
	}
	h := r.Header.Common()
	if rec.ID == "" {
		rec.ID, rec.URL = h.ID, h.URL
	}
	rec.OAIIdentifier, rec.Datestamp, rec.Sets, rec.Deleted = h.OAIIdentifier, h.Datestamp, h.Sets, h.Deleted
	return rec, nil
}

// Common converts all headers or records in the response.
func (r *Response) Common() ([]dcndl.Record, error) {
	var out []dcndl.Record
	for _, h := range r.Headers {
		out = append(out, h.Common())
	}
	for _, list := range [][]Record{r.Records, r.GetRecord} {
		for _, x := range list {
			rec, err := x.Common()
			if err != nil {
				return nil, err
			}
			out = append(out, rec)
		}
	}
	return out, nil
}

// Do performs a single OAI-PMH request. OAI-PMH errors in the body take
// precedence over the HTTP status.
func Do(ctx context.Context, c *client.Client, req Request) (*Response, []byte, error) {
	body, err := c.Get(ctx, Path, req.Values())
	if err != nil {
		var he *client.HTTPError
		if errors.As(err, &he) {
			if resp, perr := Parse(he.Body); perr == nil && len(resp.Errors) > 0 {
				if oerr := resp.Err(); oerr != nil {
					return resp, he.Body, oerr
				}
			}
		}
		return nil, nil, err
	}
	resp, err := Parse(body)
	if err != nil {
		return nil, body, err
	}
	return resp, body, resp.Err()
}
