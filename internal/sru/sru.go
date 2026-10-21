// Package sru implements the NDL Search SRU (searchRetrieve) API.
//
// Spec: 外部提供インタフェース仕様書 第1.5版 §3.
package sru

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/nightswinger/ndl/internal/client"
	"github.com/nightswinger/ndl/internal/dcndl"
)

const (
	// Path is the SRU endpoint relative to the API base URL.
	Path = "/sru"
	// MaxRecords is the largest maximumRecords the server accepts.
	MaxRecords = 500
	// MaxPosition is the last record position the server will ever return;
	// records beyond the 500th cannot be fetched even with paging.
	MaxPosition = 500
	// MsgNoRecord is the diagnostic NDL returns for an empty result set.
	MsgNoRecord = "Record does not exist"
)

// Schemas are the recordSchema values the server accepts.
var Schemas = []string{"dcndl", "dcndl_v3", "dc"}

// Packings are the recordPacking values the server accepts.
var Packings = []string{"xml", "string"}

// Request holds searchRetrieve parameters. Zero values are omitted.
type Request struct {
	Query          string
	StartRecord    int
	MaximumRecords int
	RecordSchema   string
	RecordPacking  string
	OnlyBib        bool
	InProcess      bool
}

// Values encodes the request as URL query parameters.
func (r Request) Values() url.Values {
	v := url.Values{}
	v.Set("operation", "searchRetrieve")
	v.Set("query", r.Query)
	if r.StartRecord > 0 {
		v.Set("startRecord", strconv.Itoa(r.StartRecord))
	}
	if r.MaximumRecords > 0 {
		v.Set("maximumRecords", strconv.Itoa(r.MaximumRecords))
	}
	if r.RecordSchema != "" {
		v.Set("recordSchema", r.RecordSchema)
	}
	if r.RecordPacking != "" {
		v.Set("recordPacking", r.RecordPacking)
	}
	if r.OnlyBib {
		v.Set("onlyBib", "true")
	}
	if r.InProcess {
		v.Set("inprocess", "true")
	}
	return v
}

// Validate checks parameters against the documented limits.
func (r Request) Validate() error {
	if strings.TrimSpace(r.Query) == "" {
		return errors.New("query must not be empty")
	}
	if r.MaximumRecords < 0 || r.MaximumRecords > MaxRecords {
		return fmt.Errorf("maximumRecords must be between 1 and %d", MaxRecords)
	}
	if r.StartRecord < 0 {
		return errors.New("startRecord must be >= 1")
	}
	if r.RecordSchema != "" && !contains(Schemas, r.RecordSchema) {
		return fmt.Errorf("recordSchema must be one of %s (dcndl_simple / oai_dc are not supported by SRU)", strings.Join(Schemas, ", "))
	}
	if r.RecordPacking != "" && !contains(Packings, r.RecordPacking) {
		return fmt.Errorf("recordPacking must be one of %s", strings.Join(Packings, ", "))
	}
	if r.OnlyBib && r.RecordSchema == "dc" {
		return errors.New("onlyBib requires recordSchema dcndl or dcndl_v3")
	}
	return nil
}

// Response is a parsed searchRetrieveResponse.
type Response struct {
	Version            string       `xml:"version"`
	NumberOfRecords    int          `xml:"numberOfRecords"`
	NextRecordPosition int          `xml:"nextRecordPosition"`
	Records            []RawRecord  `xml:"records>record"`
	Diagnostics        []Diagnostic `xml:"diagnostics>diagnostic"`
}

// RawRecord is one <record> with its payload still serialized.
type RawRecord struct {
	Schema   string `xml:"recordSchema"`
	Packing  string `xml:"recordPacking"`
	Position int    `xml:"recordPosition"`
	Data     struct {
		Inner string `xml:",innerxml"`
		Text  string `xml:",chardata"`
	} `xml:"recordData"`
}

// Payload returns the record XML regardless of recordPacking.
func (r RawRecord) Payload() []byte {
	if strings.TrimSpace(r.Packing) == "string" {
		return []byte(strings.TrimSpace(r.Data.Text))
	}
	return []byte(strings.TrimSpace(r.Data.Inner))
}

// Diagnostic is one SRU diagnostic.
type Diagnostic struct {
	URI     string `xml:"uri"`
	Details string `xml:"details"`
	Message string `xml:"message"`
}

func (d Diagnostic) String() string {
	parts := []string{}
	for _, s := range []string{d.Message, d.Details, d.URI} {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " / ")
}

// DiagnosticsError reports SRU diagnostics as an error.
type DiagnosticsError struct {
	Diagnostics []Diagnostic
}

func (e *DiagnosticsError) Error() string {
	msgs := make([]string, len(e.Diagnostics))
	for i, d := range e.Diagnostics {
		msgs[i] = d.String()
	}
	return "SRU diagnostic: " + strings.Join(msgs, "; ")
}

// Parse decodes a searchRetrieveResponse body.
func Parse(body []byte) (*Response, error) {
	var resp Response
	if err := xml.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse SRU response: %w", err)
	}
	return &resp, nil
}

// Err returns a *DiagnosticsError if the response carries diagnostics.
// NDL reports an empty result set as the diagnostic "Record does not
// exist"; that case is normalized to zero records and is not an error.
func (r *Response) Err() error {
	var real []Diagnostic
	for _, d := range r.Diagnostics {
		if strings.EqualFold(strings.TrimSpace(d.Message), MsgNoRecord) {
			continue
		}
		real = append(real, d)
	}
	if len(real) > 0 {
		return &DiagnosticsError{Diagnostics: real}
	}
	return nil
}

// Empty reports whether the server said no record matched.
func (r *Response) Empty() bool {
	for _, d := range r.Diagnostics {
		if strings.EqualFold(strings.TrimSpace(d.Message), MsgNoRecord) {
			return true
		}
	}
	return r.NumberOfRecords == 0
}

// Decode converts the raw records into common Records.
func (r *Response) Decode() ([]dcndl.Record, error) {
	out := make([]dcndl.Record, 0, len(r.Records))
	for _, raw := range r.Records {
		rec, err := dcndl.Parse(raw.Payload())
		if err != nil {
			return nil, fmt.Errorf("record %d: %w", raw.Position, err)
		}
		out = append(out, rec)
	}
	return out, nil
}

// Search performs one searchRetrieve request. The raw body is returned
// alongside the parsed response (for --format xml). Diagnostics in the
// body take precedence over the HTTP status.
func Search(ctx context.Context, c *client.Client, req Request) (*Response, []byte, error) {
	body, err := c.Get(ctx, Path, req.Values())
	if err != nil {
		var he *client.HTTPError
		if errors.As(err, &he) {
			if resp, perr := Parse(he.Body); perr == nil && len(resp.Diagnostics) > 0 {
				if derr := resp.Err(); derr != nil {
					return resp, he.Body, derr
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

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
