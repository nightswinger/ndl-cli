// Package opensearch implements the NDL Search OpenSearch API (RSS 2.0).
//
// Spec: 外部提供インタフェース仕様書 第1.5版 §4. The server silently
// ignores unknown parameter names and answers malformed values with zero
// hits, so Warnings() helps surface likely mistakes.
package opensearch

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
	// Path is the OpenSearch endpoint relative to the API base URL.
	Path = "/opensearch"
	// MaxCnt is the largest cnt the server honours; larger values return 0 hits.
	MaxCnt = 500
	// MaxPosition is the last retrievable record position.
	MaxPosition = 500
	// suspiciousTotal is a hit count that suggests every condition was ignored.
	suspiciousTotal = 50_000_000
)

// Params are the OpenSearch parameters of spec table 4-1.
type Params struct {
	DPID               []string
	DPGroupID          string
	Any                string
	Title              string
	Creator            string
	Publisher          string
	DigitizedPublisher string
	NDC                string
	From               string
	Until              string
	ISBN               string
	MediaType          []string
	Cnt                int
	Idx                int
}

// Values encodes the parameters. Multiple dpid/mediatype values are
// space-separated, which the server treats as OR.
func (p Params) Values() url.Values {
	v := url.Values{}
	set := func(k, s string) {
		if s = strings.TrimSpace(s); s != "" {
			v.Set(k, s)
		}
	}
	set("dpid", strings.Join(p.DPID, " "))
	set("dpgroupid", p.DPGroupID)
	set("any", p.Any)
	set("title", p.Title)
	set("creator", p.Creator)
	set("publisher", p.Publisher)
	set("digitized_publisher", p.DigitizedPublisher)
	set("ndc", p.NDC)
	set("from", p.From)
	set("until", p.Until)
	set("isbn", p.ISBN)
	set("mediatype", strings.Join(p.MediaType, " "))
	if p.Cnt > 0 {
		v.Set("cnt", strconv.Itoa(p.Cnt))
	}
	if p.Idx > 0 {
		v.Set("idx", strconv.Itoa(p.Idx))
	}
	return v
}

// HasCondition reports whether a search condition other than dpid /
// dpgroupid / paging is present. The server rejects dpid-only searches.
func (p Params) HasCondition() bool {
	v := p.Values()
	for _, k := range []string{"dpid", "dpgroupid", "cnt", "idx"} {
		v.Del(k)
	}
	return len(v) > 0
}

// Validate checks documented limits.
func (p Params) Validate() error {
	if !p.HasCondition() {
		return errors.New("at least one search condition is required (dpid alone cannot be searched)")
	}
	if p.Cnt < 0 || p.Cnt > MaxCnt {
		return fmt.Errorf("cnt must be between 1 and %d (larger values silently return 0 hits)", MaxCnt)
	}
	if p.Idx < 0 || p.Idx > MaxPosition {
		return fmt.Errorf("idx must be between 1 and %d (records after the 500th cannot be retrieved)", MaxPosition)
	}
	if p.From != "" && p.Until != "" && len(p.From) != len(p.Until) {
		return fmt.Errorf("from and until must use the same date format (%q vs %q)", p.From, p.Until)
	}
	return nil
}

// Feed is a parsed RSS response.
type Feed struct {
	Title        string `xml:"channel>title"`
	Link         string `xml:"channel>link"`
	TotalResults int    `xml:"channel>totalResults"`
	StartIndex   int    `xml:"channel>startIndex"`
	ItemsPerPage int    `xml:"channel>itemsPerPage"`
	Items        []Item `xml:"channel>item"`
}

type typed struct {
	Type  string `xml:"http://www.w3.org/2001/XMLSchema-instance type,attr"`
	Value string `xml:",chardata"`
}

// Item is one RSS <item> with the NDL dc/dcndl extensions.
type Item struct {
	Title              string   `xml:"title"`
	Link               string   `xml:"link"`
	GUID               string   `xml:"guid"`
	Author             string   `xml:"author"`
	Categories         []string `xml:"category"`
	PubDate            string   `xml:"pubDate"`
	DCTitle            string   `xml:"http://purl.org/dc/elements/1.1/ title"`
	TitleTranscription string   `xml:"http://ndl.go.jp/dcndl/terms/ titleTranscription"`
	Creators           []string `xml:"http://purl.org/dc/elements/1.1/ creator"`
	Publishers         []string `xml:"http://purl.org/dc/elements/1.1/ publisher"`
	Dates              []typed  `xml:"http://purl.org/dc/elements/1.1/ date"`
	Issued             []string `xml:"http://purl.org/dc/terms/ issued"`
	Identifiers        []typed  `xml:"http://purl.org/dc/elements/1.1/ identifier"`
	Subjects           []typed  `xml:"http://purl.org/dc/elements/1.1/ subject"`
	SeeAlso            []struct {
		Resource string `xml:"http://www.w3.org/1999/02/22-rdf-syntax-ns# resource,attr"`
	} `xml:"http://www.w3.org/2000/01/rdf-schema# seeAlso"`
}

// Record maps the item onto the common record.
func (it Item) Record() dcndl.Record {
	var r dcndl.Record
	r.SetURL(it.Link)
	if r.URL == "" {
		r.SetURL(it.GUID)
	}
	r.Title = strings.TrimSpace(it.DCTitle)
	if r.Title == "" {
		r.Title = strings.TrimSpace(it.Title)
	}
	r.TitleTranscription = strings.TrimSpace(it.TitleTranscription)
	for _, c := range it.Creators {
		r.Creators = dcndl.AppendUniq(r.Creators, c)
	}
	if len(r.Creators) == 0 && strings.TrimSpace(it.Author) != "" {
		r.Creators = []string{strings.TrimSpace(it.Author)}
	}
	for _, p := range it.Publishers {
		r.Publishers = dcndl.AppendUniq(r.Publishers, p)
	}
	for _, d := range it.Dates {
		if r.Date == "" {
			r.Date = strings.TrimSpace(d.Value)
		}
	}
	if r.Date == "" && len(it.Issued) > 0 {
		r.Date = strings.TrimSpace(it.Issued[0])
	}
	for _, id := range it.Identifiers {
		switch typeName(id.Type) {
		case "ISBN", "ISBN13":
			r.ISBN = dcndl.AppendUniq(r.ISBN, id.Value)
		case "ISSN", "ISSNL":
			r.ISSN = dcndl.AppendUniq(r.ISSN, id.Value)
		case "JPNO":
			r.JPNO = dcndl.AppendUniq(r.JPNO, id.Value)
		case "NDLBibID":
			r.NDLBibID = dcndl.AppendUniq(r.NDLBibID, id.Value)
		}
	}
	for _, s := range it.Subjects {
		t := typeName(s.Type)
		switch {
		case strings.HasPrefix(t, "NDC"):
			r.NDC = dcndl.AppendUniq(r.NDC, s.Value)
		case t == "NDLC":
			r.NDLC = dcndl.AppendUniq(r.NDLC, s.Value)
		case t == "":
			r.Subjects = dcndl.AppendUniq(r.Subjects, s.Value)
		}
	}
	for _, c := range it.Categories {
		r.MaterialType = dcndl.AppendUniq(r.MaterialType, c)
	}
	for _, s := range it.SeeAlso {
		if s.Resource != "" && !strings.HasPrefix(s.Resource, dcndl.SearchURLBase) {
			r.SeeAlso = dcndl.AppendUniq(r.SeeAlso, s.Resource)
		}
	}
	return r
}

// Records converts all items.
func (f *Feed) Records() []dcndl.Record {
	out := make([]dcndl.Record, len(f.Items))
	for i, it := range f.Items {
		out[i] = it.Record()
	}
	return out
}

// typeName turns "dcndl:ISBN" into "ISBN".
func typeName(t string) string {
	if i := strings.LastIndexByte(t, ':'); i >= 0 {
		return t[i+1:]
	}
	return t
}

// APIError is NDL's generic <error><code/><message/></error> body.
type APIError struct {
	Code    string `xml:"code"`
	Message string `xml:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("OpenSearch error %s: %s", e.Code, e.Message)
}

// Parse decodes an OpenSearch response body.
func Parse(body []byte) (*Feed, error) {
	var root struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("parse OpenSearch response: %w", err)
	}
	if root.XMLName.Local == "error" {
		var e APIError
		if err := xml.Unmarshal(body, &e); err != nil {
			return nil, err
		}
		return nil, &e
	}
	if root.XMLName.Local != "rss" {
		return nil, fmt.Errorf("unexpected OpenSearch root element <%s>", root.XMLName.Local)
	}
	var f Feed
	if err := xml.Unmarshal(body, &f); err != nil {
		return nil, fmt.Errorf("parse OpenSearch response: %w", err)
	}
	return &f, nil
}

// Search performs one OpenSearch request and returns the parsed feed and raw body.
func Search(ctx context.Context, c *client.Client, p Params) (*Feed, []byte, error) {
	body, err := c.Get(ctx, Path, p.Values())
	if err != nil {
		var he *client.HTTPError
		if errors.As(err, &he) {
			var e APIError
			if xml.Unmarshal(he.Body, &e) == nil && e.Code != "" && he.StatusCode < 500 && he.StatusCode != 429 {
				return nil, he.Body, &e
			}
		}
		return nil, nil, err
	}
	f, err := Parse(body)
	return f, body, err
}

// Warnings returns hints about likely parameter mistakes, since the API
// never reports parameter errors.
func Warnings(p Params, f *Feed) []string {
	var w []string
	if f.TotalResults == 0 {
		w = append(w, "0 hits. OpenSearch does not report parameter errors: check for typos in parameter values, "+
			"date formats (from/until must share YYYY / YYYY-MM / YYYY-MM-DD), and unknown dpid / mediatype values.")
	} else if f.TotalResults >= suspiciousTotal && p.HasCondition() {
		w = append(w, fmt.Sprintf("%d hits: the server may have ignored your conditions (unknown parameter names are silently dropped).", f.TotalResults))
	}
	if p.DPGroupID != "" {
		w = append(w, "dpgroupid was discontinued in December 2023; use a data group ID with --dpid instead.")
	}
	return w
}
