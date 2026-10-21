// Package output renders records as table, json or jsonl. The xml format
// (raw API responses) is written directly by the commands.
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/nightswinger/ndl/internal/dcndl"
	"golang.org/x/term"
)

// Formats are the supported --format values.
var Formats = []string{"table", "json", "jsonl", "xml"}

// DefaultTableFields are the columns shown by the table format.
var DefaultTableFields = []string{"title", "creators", "publishers", "date", "identifier", "url"}

// fieldAliases maps convenience names to Record JSON names.
var fieldAliases = map[string]string{
	"creator":   "creators",
	"author":    "creators",
	"publisher": "publishers",
	"year":      "date",
	"ndl_url":   "url",
	"link":      "url",
}

// ValidFormat reports whether f is a supported format.
func ValidFormat(f string) bool {
	for _, x := range Formats {
		if x == f {
			return true
		}
	}
	return false
}

// NormalizeFields resolves aliases and rejects unknown names.
// "identifier" is a computed field (JP number, ISBN, ISSN or item ID).
func NormalizeFields(fields []string) ([]string, error) {
	var out []string
	for _, f := range fields {
		f = strings.ToLower(strings.TrimSpace(f))
		if f == "" {
			continue
		}
		if a, ok := fieldAliases[f]; ok {
			f = a
		}
		if f != "identifier" && !contains(dcndl.FieldNames, f) {
			return nil, fmt.Errorf("unknown field %q (valid: identifier, %s)", f, strings.Join(dcndl.FieldNames, ", "))
		}
		out = append(out, f)
	}
	return out, nil
}

// Meta is result-set information included in the json format.
type Meta struct {
	Source             string `json:"source"`
	Query              string `json:"query,omitempty"`
	NumberOfRecords    *int   `json:"numberOfRecords,omitempty"`
	NextRecordPosition *int   `json:"nextRecordPosition,omitempty"`
	ResumptionToken    string `json:"resumptionToken,omitempty"`
	CompleteListSize   *int   `json:"completeListSize,omitempty"`
	Returned           int    `json:"returned"`
}

// Int returns a pointer to n, for optional Meta fields.
func Int(n int) *int { return &n }

// Options control rendering.
type Options struct {
	Format string
	Fields []string // normalized; empty means all (json) or DefaultTableFields (table)
	Width  int      // table width; 0 = detect
}

// Records writes recs in the requested format.
func Records(w io.Writer, recs []dcndl.Record, meta Meta, opt Options) error {
	switch opt.Format {
	case "json":
		return JSON(w, recs, meta, opt.Fields)
	case "jsonl":
		return JSONL(w, recs, opt.Fields)
	case "table", "":
		fields := opt.Fields
		if len(fields) == 0 {
			fields = DefaultTableFields
		}
		return Table(w, recs, fields, opt.Width)
	}
	return fmt.Errorf("format %q cannot render records", opt.Format)
}

// JSON writes {"meta": ..., "records": [...]}.
func JSON(w io.Writer, recs []dcndl.Record, meta Meta, fields []string) error {
	meta.Returned = len(recs)
	items := make([]any, len(recs))
	for i := range recs {
		items[i] = project(&recs[i], fields)
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		Meta    Meta  `json:"meta"`
		Records []any `json:"records"`
	}{meta, items})
}

// JSONL writes one JSON object per line, suitable for appending.
func JSONL(w io.Writer, recs []dcndl.Record, fields []string) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for i := range recs {
		if err := enc.Encode(project(&recs[i], fields)); err != nil {
			return err
		}
	}
	return nil
}

// project returns the record itself, or an ordered subset of its fields.
func project(r *dcndl.Record, fields []string) any {
	if len(fields) == 0 {
		return r
	}
	b, _ := json.Marshal(r)
	var all map[string]json.RawMessage
	_ = json.Unmarshal(b, &all)
	om := orderedMap{}
	for _, f := range fields {
		if f == "identifier" {
			if id := r.Identifier(); id != "" {
				v, _ := json.Marshal(id)
				om = append(om, kv{f, v})
			}
			continue
		}
		if v, ok := all[f]; ok {
			om = append(om, kv{f, v})
		}
	}
	return om
}

type kv struct {
	k string
	v json.RawMessage
}

type orderedMap []kv

func (m orderedMap) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, e := range m {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(e.k)
		b.Write(k)
		b.WriteByte(':')
		b.Write(e.v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Value returns a field of r as display text.
func Value(r *dcndl.Record, field string) string {
	switch field {
	case "identifier":
		return r.Identifier()
	case "id":
		return r.ID
	case "title":
		return r.Title
	case "title_transcription":
		return r.TitleTranscription
	case "creators":
		return join(r.Creators)
	case "publishers":
		return join(r.Publishers)
	case "date":
		return r.Date
	case "isbn":
		return join(r.ISBN)
	case "issn":
		return join(r.ISSN)
	case "jpno":
		return join(r.JPNO)
	case "ndlbibid":
		return join(r.NDLBibID)
	case "ndc":
		return join(r.NDC)
	case "ndlc":
		return join(r.NDLC)
	case "subjects":
		return join(r.Subjects)
	case "material_type":
		return join(r.MaterialType)
	case "language":
		return join(r.Language)
	case "url":
		return r.URL
	case "see_also":
		return join(r.SeeAlso)
	case "oai_identifier":
		return r.OAIIdentifier
	case "datestamp":
		return r.Datestamp
	case "sets":
		return join(r.Sets)
	case "deleted":
		if r.Deleted {
			return "deleted"
		}
		return ""
	}
	return ""
}

func join(s []string) string { return strings.Join(s, "; ") }

var headers = map[string]string{
	"creators":       "CREATOR",
	"publishers":     "PUBLISHER",
	"identifier":     "ID",
	"oai_identifier": "OAI ID",
}

// Table writes records as an aligned table fitted to width.
func Table(w io.Writer, recs []dcndl.Record, fields []string, width int) error {
	hdr := make([]string, len(fields))
	for i, f := range fields {
		if h, ok := headers[f]; ok {
			hdr[i] = h
		} else {
			hdr[i] = strings.ToUpper(f)
		}
	}
	rows := make([][]string, len(recs))
	for i := range recs {
		row := make([]string, len(fields))
		for j, f := range fields {
			row[j] = Value(&recs[i], f)
		}
		rows[i] = row
	}
	flex := map[int]int{}
	for i, f := range fields {
		switch f {
		case "title", "title_transcription", "subjects", "see_also":
			flex[i] = 3
		case "creators", "publishers", "material_type", "sets":
			flex[i] = 2
		}
	}
	return Grid(w, hdr, rows, flex, width)
}

// Grid writes a generic aligned table. flex maps column index to a weight;
// only weighted columns are shrunk (with an ellipsis) to fit width.
func Grid(w io.Writer, hdr []string, rows [][]string, flex map[int]int, width int) error {
	if width <= 0 {
		width = TerminalWidth()
	}
	n := len(hdr)
	nat := make([]int, n)
	for i, h := range hdr {
		nat[i] = StringWidth(h)
	}
	for _, r := range rows {
		for i, c := range r {
			r[i] = oneLine(c)
			if cw := StringWidth(r[i]); cw > nat[i] {
				nat[i] = cw
			}
		}
	}
	widths := fit(nat, flex, width-2*(n-1))
	line := func(cells []string) error {
		var b strings.Builder
		for i, c := range cells {
			c = Truncate(c, widths[i])
			b.WriteString(c)
			if i < n-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-StringWidth(c)+2))
			}
		}
		_, err := fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
		return err
	}
	if err := line(hdr); err != nil {
		return err
	}
	for _, r := range rows {
		if err := line(r); err != nil {
			return err
		}
	}
	return nil
}

// fit shrinks weighted columns so the total fits avail.
func fit(nat []int, flex map[int]int, avail int) []int {
	const minWidth = 8
	widths := append([]int(nil), nat...)
	total := 0
	for _, x := range nat {
		total += x
	}
	if total <= avail || len(flex) == 0 {
		return widths
	}
	space := avail
	for i, x := range nat {
		if _, ok := flex[i]; !ok {
			space -= x
		}
	}
	pending := map[int]int{}
	for i, wgt := range flex {
		pending[i] = wgt
	}
	// Columns narrower than their share keep their natural width; the rest
	// split what remains by weight.
	for {
		sumW := 0
		for _, wgt := range pending {
			sumW += wgt
		}
		if sumW == 0 {
			break
		}
		changed := false
		for i, wgt := range pending {
			if share := space * wgt / sumW; nat[i] <= share {
				widths[i] = nat[i]
				space -= nat[i]
				delete(pending, i)
				changed = true
			}
		}
		if changed {
			continue
		}
		for i, wgt := range pending {
			widths[i] = space * wgt / sumW
			if widths[i] < minWidth {
				widths[i] = minWidth
			}
		}
		break
	}
	return widths
}

// TerminalWidth returns the stdout terminal width, $COLUMNS, or 120.
func TerminalWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	if n, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && n > 0 {
		return n
	}
	return 120
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Truncate shortens s to at most width display columns, adding "…".
func Truncate(s string, width int) string {
	if StringWidth(s) <= width {
		return s
	}
	if width <= 0 {
		return ""
	}
	if width == 1 {
		return "…"
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := RuneWidth(r)
		if used+rw > width-1 {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	return b.String() + "…"
}

// StringWidth is the display width of s in a monospace terminal.
func StringWidth(s string) int {
	n := 0
	for _, r := range s {
		n += RuneWidth(r)
	}
	return n
}

// RuneWidth returns 2 for East Asian wide/fullwidth runes, 0 for
// combining marks and 1 otherwise.
func RuneWidth(r rune) int {
	switch {
	case r == 0 || r < 32 || (r >= 0x7f && r < 0xa0):
		return 0
	case r >= 0x300 && r <= 0x36f, r >= 0x3099 && r <= 0x309a, r == 0x200b:
		return 0
	case r >= 0x1100 && r <= 0x115f,
		r >= 0x2e80 && r <= 0x303e,
		r >= 0x3041 && r <= 0x33ff,
		r >= 0x3400 && r <= 0x4dbf,
		r >= 0x4e00 && r <= 0x9fff,
		r >= 0xa000 && r <= 0xa4cf,
		r >= 0xac00 && r <= 0xd7a3,
		r >= 0xf900 && r <= 0xfaff,
		r >= 0xfe30 && r <= 0xfe4f,
		r >= 0xff00 && r <= 0xff60,
		r >= 0xffe0 && r <= 0xffe6,
		r >= 0x1f300 && r <= 0x1f64f,
		r >= 0x1f900 && r <= 0x1f9ff,
		r >= 0x20000 && r <= 0x3fffd:
		return 2
	}
	return 1
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
