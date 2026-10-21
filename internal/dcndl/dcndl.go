// Package dcndl defines the common bibliographic record shared by all
// subcommands and parsers for the metadata formats NDL Search returns:
// DC-NDL (RDF) ver.2 "dcndl", ver.3 "dcndl_v3", and simple Dublin Core
// (SRU "dc" / OAI-PMH "oai_dc").
//
// See https://ndlsearch.ndl.go.jp/renkei/dcndl for the format specification.
package dcndl

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

// SearchURLBase is the prefix of NDL Search item pages.
const SearchURLBase = "https://ndlsearch.ndl.go.jp/books/"

// Record is a simplified bibliographic record.
type Record struct {
	// ID is the NDL Search item token, e.g. "R100000002-I000007305653".
	ID                 string   `json:"id,omitempty"`
	Title              string   `json:"title,omitempty"`
	TitleTranscription string   `json:"title_transcription,omitempty"`
	Creators           []string `json:"creators,omitempty"`
	Publishers         []string `json:"publishers,omitempty"`
	// Date is the publication date (W3CDTF when available).
	Date         string   `json:"date,omitempty"`
	ISBN         []string `json:"isbn,omitempty"`
	ISSN         []string `json:"issn,omitempty"`
	JPNO         []string `json:"jpno,omitempty"`
	NDLBibID     []string `json:"ndlbibid,omitempty"`
	NDC          []string `json:"ndc,omitempty"`
	NDLC         []string `json:"ndlc,omitempty"`
	Subjects     []string `json:"subjects,omitempty"`
	MaterialType []string `json:"material_type,omitempty"`
	Language     []string `json:"language,omitempty"`
	// URL is the NDL Search page for the item.
	URL     string   `json:"url,omitempty"`
	SeeAlso []string `json:"see_also,omitempty"`

	// OAI-PMH header fields.
	OAIIdentifier string   `json:"oai_identifier,omitempty"`
	Datestamp     string   `json:"datestamp,omitempty"`
	Sets          []string `json:"sets,omitempty"`
	Deleted       bool     `json:"deleted,omitempty"`
}

// FieldNames lists the JSON names of Record fields, in declaration order.
var FieldNames = []string{
	"id", "title", "title_transcription", "creators", "publishers", "date",
	"isbn", "issn", "jpno", "ndlbibid", "ndc", "ndlc", "subjects",
	"material_type", "language", "url", "see_also",
	"oai_identifier", "datestamp", "sets", "deleted",
}

// Identifier returns the most useful identifier for display:
// JP number first, then ISBN, ISSN, then the NDL Search item ID.
func (r *Record) Identifier() string {
	switch {
	case len(r.JPNO) > 0:
		return "JP:" + r.JPNO[0]
	case len(r.ISBN) > 0:
		return "ISBN:" + r.ISBN[0]
	case len(r.ISSN) > 0:
		return "ISSN:" + r.ISSN[0]
	}
	return r.ID
}

// SetURL fills URL (and ID) from an NDL Search URL or item token.
func (r *Record) SetURL(s string) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return
	}
	if strings.HasPrefix(s, SearchURLBase) {
		r.URL = s
		if r.ID == "" {
			r.ID = strings.TrimPrefix(s, SearchURLBase)
		}
		return
	}
	if !strings.Contains(s, "/") && strings.HasPrefix(s, "R") {
		r.ID = s
		r.URL = SearchURLBase + s
	}
}

// ItemFromOAIIdentifier extracts the item token from an OAI-PMH identifier
// such as "oai:ndlsearch.ndl.go.jp:R000000014-I000005card5000005".
func ItemFromOAIIdentifier(id string) string {
	if i := strings.LastIndexByte(id, ':'); i >= 0 {
		return id[i+1:]
	}
	return id
}

const nsRDF = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"

type rdfDoc struct {
	Admin []struct {
		About string `xml:"http://www.w3.org/1999/02/22-rdf-syntax-ns# about,attr"`
	} `xml:"http://ndl.go.jp/dcndl/terms/ BibAdminResource"`
	Bib []bibResource `xml:"http://ndl.go.jp/dcndl/terms/ BibResource"`
}

type typed struct {
	Datatype string `xml:"http://www.w3.org/1999/02/22-rdf-syntax-ns# datatype,attr"`
	Value    string `xml:",chardata"`
}

type resource struct {
	Resource string `xml:"http://www.w3.org/1999/02/22-rdf-syntax-ns# resource,attr"`
	Label    string `xml:"http://www.w3.org/2000/01/rdf-schema# label,attr"`
	// Value is set for <x><rdf:Description><rdf:value>...</rdf:value></rdf:Description></x>.
	Value string `xml:"Description>value"`
	Text  string `xml:",chardata"`
}

func (r resource) text() string {
	if v := strings.TrimSpace(r.Value); v != "" {
		return v
	}
	return strings.TrimSpace(r.Text)
}

type agent struct {
	Name string `xml:"Agent>name"`
	Text string `xml:",chardata"`
}

func (a agent) text() string {
	if v := strings.TrimSpace(a.Name); v != "" {
		return v
	}
	return strings.TrimSpace(a.Text)
}

type bibResource struct {
	About       string  `xml:"http://www.w3.org/1999/02/22-rdf-syntax-ns# about,attr"`
	Identifiers []typed `xml:"http://purl.org/dc/terms/ identifier"`
	Title       string  `xml:"http://purl.org/dc/terms/ title"`
	DCTitle     []struct {
		Value         string `xml:"Description>value"`
		Transcription string `xml:"Description>transcription"`
		Text          string `xml:",chardata"`
	} `xml:"http://purl.org/dc/elements/1.1/ title"`
	Creators     []agent    `xml:"http://purl.org/dc/terms/ creator"`
	DCCreators   []string   `xml:"http://purl.org/dc/elements/1.1/ creator"`
	Publishers   []agent    `xml:"http://purl.org/dc/terms/ publisher"`
	Dates        []string   `xml:"http://purl.org/dc/terms/ date"`
	Issued       []typed    `xml:"http://purl.org/dc/terms/ issued"`
	Subjects     []resource `xml:"http://purl.org/dc/terms/ subject"`
	Languages    []string   `xml:"http://purl.org/dc/terms/ language"`
	MaterialType []resource `xml:"http://ndl.go.jp/dcndl/terms/ materialType"`
	SeeAlso      []resource `xml:"http://www.w3.org/2000/01/rdf-schema# seeAlso"`
}

// ParseRDF parses one <rdf:RDF> document (dcndl or dcndl_v3) into a Record.
func ParseRDF(data []byte) (Record, error) {
	var doc rdfDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return Record{}, fmt.Errorf("parse DC-NDL RDF: %w", err)
	}
	var r Record
	for _, a := range doc.Admin {
		r.SetURL(a.About)
	}
	for _, b := range doc.Bib {
		r.SetURL(b.About)
		for _, id := range b.Identifiers {
			v := strings.TrimSpace(id.Value)
			switch localType(id.Datatype) {
			case "ISBN", "ISBN13", "SetISBN":
				r.ISBN = appendUniq(r.ISBN, v)
			case "ISSN", "ISSNL":
				r.ISSN = appendUniq(r.ISSN, v)
			case "JPNO":
				r.JPNO = appendUniq(r.JPNO, v)
			case "NDLBibID":
				r.NDLBibID = appendUniq(r.NDLBibID, v)
			}
		}
		if r.Title == "" {
			r.Title = strings.TrimSpace(b.Title)
		}
		for _, t := range b.DCTitle {
			if r.Title == "" {
				r.Title = firstNonEmpty(t.Value, t.Text)
			}
			if r.TitleTranscription == "" {
				r.TitleTranscription = strings.TrimSpace(t.Transcription)
			}
		}
		for _, c := range b.Creators {
			r.Creators = appendUniq(r.Creators, c.text())
		}
		if len(b.Creators) == 0 {
			for _, c := range b.DCCreators {
				r.Creators = appendUniq(r.Creators, c)
			}
		}
		for _, p := range b.Publishers {
			r.Publishers = appendUniq(r.Publishers, p.text())
		}
		if r.Date == "" {
			for _, d := range b.Issued {
				if v := strings.TrimSpace(d.Value); v != "" {
					r.Date = v
					break
				}
			}
		}
		if r.Date == "" && len(b.Dates) > 0 {
			r.Date = strings.TrimSpace(b.Dates[0])
		}
		for _, s := range b.Subjects {
			if s.Resource != "" {
				scheme, code := classification(s.Resource)
				switch {
				case strings.HasPrefix(scheme, "ndc"):
					r.NDC = appendUniq(r.NDC, code)
				case scheme == "ndlc":
					r.NDLC = appendUniq(r.NDLC, code)
				}
				continue
			}
			r.Subjects = appendUniq(r.Subjects, s.text())
		}
		for _, l := range b.Languages {
			r.Language = appendUniq(r.Language, l)
		}
		for _, m := range b.MaterialType {
			r.MaterialType = appendUniq(r.MaterialType, firstNonEmpty(m.Label, m.Resource))
		}
		for _, s := range b.SeeAlso {
			if s.Resource != "" && !strings.HasPrefix(s.Resource, SearchURLBase) {
				r.SeeAlso = appendUniq(r.SeeAlso, s.Resource)
			}
		}
	}
	return r, nil
}

type dcDoc struct {
	Titles      []string `xml:"http://purl.org/dc/elements/1.1/ title"`
	Creators    []string `xml:"http://purl.org/dc/elements/1.1/ creator"`
	Publishers  []string `xml:"http://purl.org/dc/elements/1.1/ publisher"`
	Dates       []string `xml:"http://purl.org/dc/elements/1.1/ date"`
	Identifiers []string `xml:"http://purl.org/dc/elements/1.1/ identifier"`
	Subjects    []string `xml:"http://purl.org/dc/elements/1.1/ subject"`
	Languages   []string `xml:"http://purl.org/dc/elements/1.1/ language"`
	Types       []string `xml:"http://purl.org/dc/elements/1.1/ type"`
}

// ParseDC parses a simple Dublin Core record (srw_dc:dc or oai_dc:dc).
func ParseDC(data []byte) (Record, error) {
	var doc dcDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return Record{}, fmt.Errorf("parse Dublin Core: %w", err)
	}
	var r Record
	if len(doc.Titles) > 0 {
		r.Title = strings.TrimSpace(doc.Titles[0])
	}
	for _, v := range doc.Creators {
		r.Creators = appendUniq(r.Creators, v)
	}
	for _, v := range doc.Publishers {
		r.Publishers = appendUniq(r.Publishers, v)
	}
	if len(doc.Dates) > 0 {
		r.Date = strings.TrimSpace(doc.Dates[0])
	}
	for _, v := range doc.Identifiers {
		v = strings.TrimSpace(v)
		switch {
		case strings.HasPrefix(v, SearchURLBase):
			r.SetURL(v)
		case strings.HasPrefix(strings.ToUpper(v), "ISBN:"):
			r.ISBN = appendUniq(r.ISBN, v[5:])
		case strings.HasPrefix(strings.ToUpper(v), "JP:"), strings.HasPrefix(strings.ToUpper(v), "JPNO:"):
			r.JPNO = appendUniq(r.JPNO, v[strings.IndexByte(v, ':')+1:])
		}
	}
	for _, v := range doc.Subjects {
		r.Subjects = appendUniq(r.Subjects, v)
	}
	for _, v := range doc.Languages {
		r.Language = appendUniq(r.Language, v)
	}
	for _, v := range doc.Types {
		r.MaterialType = appendUniq(r.MaterialType, v)
	}
	return r, nil
}

// Parse dispatches on the root element: <rdf:RDF> or a Dublin Core record.
func Parse(data []byte) (Record, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return Record{}, fmt.Errorf("parse record: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok {
			if se.Name.Space == nsRDF && se.Name.Local == "RDF" {
				return ParseRDF(data)
			}
			return ParseDC(data)
		}
	}
}

// localType returns the last path segment of an rdf:datatype URI.
func localType(dt string) string {
	if i := strings.LastIndexAny(dt, "/#"); i >= 0 {
		return dt[i+1:]
	}
	return dt
}

// classification splits "http://id.ndl.go.jp/class/ndc9/913.6" into ("ndc9", "913.6").
func classification(uri string) (scheme, code string) {
	const prefix = "/class/"
	i := strings.Index(uri, prefix)
	if i < 0 {
		return "", ""
	}
	rest := uri[i+len(prefix):]
	j := strings.IndexByte(rest, '/')
	if j < 0 {
		return "", ""
	}
	return strings.ToLower(rest[:j]), rest[j+1:]
}

func appendUniq(list []string, v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return list
	}
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// AppendUniq appends v to list unless it is blank or already present.
func AppendUniq(list []string, v string) []string { return appendUniq(list, v) }

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}
