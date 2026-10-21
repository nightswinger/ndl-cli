package sru

import (
	"fmt"
	"regexp"
	"strings"
)

// Fields are the helper search fields that are turned into CQL.
// Multi-valued fields (DPID, MediaType) are OR-ed by the server when
// given as one space-separated value.
type Fields struct {
	Title              string
	Creator            string
	Publisher          string
	DigitizedPublisher string
	Subject            string
	Description        string
	Anywhere           string
	ISBN               string
	ISSN               string
	JPNO               string
	NDC                string
	NDLC               string
	From               string
	Until              string
	DPID               []string
	MediaType          []string
}

// Empty reports whether no field is set.
func (f Fields) Empty() bool {
	return len(f.pairs()) == 0
}

func (f Fields) pairs() [][2]string {
	var p [][2]string
	add := func(index, value string) {
		if v := strings.TrimSpace(value); v != "" {
			p = append(p, [2]string{index, v})
		}
	}
	add("title", f.Title)
	add("creator", f.Creator)
	add("publisher", f.Publisher)
	add("digitized_publisher", f.DigitizedPublisher)
	add("subject", f.Subject)
	add("description", f.Description)
	add("anywhere", f.Anywhere)
	add("isbn", f.ISBN)
	add("issn", f.ISSN)
	add("jpno", f.JPNO)
	add("ndc", f.NDC)
	add("ndlc", f.NDLC)
	add("from", f.From)
	add("until", f.Until)
	add("dpid", strings.Join(f.DPID, " "))
	add("mediatype", strings.Join(f.MediaType, " "))
	return p
}

var dateRe = regexp.MustCompile(`^\d{4}(-\d{2}(-\d{2})?)?$`)

// CQL builds an AND-joined CQL query such as `title="桜" AND from="2018"`.
func (f Fields) CQL() (string, error) {
	if f.From != "" && !dateRe.MatchString(f.From) {
		return "", fmt.Errorf("from must be YYYY, YYYY-MM or YYYY-MM-DD: %q", f.From)
	}
	if f.Until != "" && !dateRe.MatchString(f.Until) {
		return "", fmt.Errorf("until must be YYYY, YYYY-MM or YYYY-MM-DD: %q", f.Until)
	}
	if f.From != "" && f.Until != "" && len(f.From) != len(f.Until) {
		return "", fmt.Errorf("from and until must use the same date format (%q vs %q)", f.From, f.Until)
	}
	pairs := f.pairs()
	if len(pairs) == 0 {
		return "", fmt.Errorf("no search fields given")
	}
	terms := make([]string, len(pairs))
	for i, p := range pairs {
		terms[i] = p[0] + "=" + Quote(p[1])
	}
	return strings.Join(terms, " AND "), nil
}

// Quote returns v as a CQL quoted string, escaping \ and ".
func Quote(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return `"` + v + `"`
}
