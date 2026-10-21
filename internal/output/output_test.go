package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nightswinger/ndl/internal/dcndl"
)

var recs = []dcndl.Record{
	{
		ID: "R100000002-I000007305653", Title: "吾輩は猫である", Creators: []string{"夏目, 漱石"},
		Publishers: []string{"新潮社"}, Date: "2003", ISBN: []string{"4-10-101001-3"}, JPNO: []string{"20570720"},
		URL: "https://ndlsearch.ndl.go.jp/books/R100000002-I000007305653",
	},
	{
		ID: "R100000136-I1", Title: strings.Repeat("とても長いタイトル", 10), Creators: []string{"A", "B"},
		URL: "https://ndlsearch.ndl.go.jp/books/R100000136-I1",
	},
}

func TestJSON(t *testing.T) {
	var b bytes.Buffer
	if err := JSON(&b, recs, Meta{Source: "sru", NumberOfRecords: Int(2), NextRecordPosition: Int(0)}, nil); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Meta    map[string]any   `json:"meta"`
		Records []map[string]any `json:"records"`
	}
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Meta["numberOfRecords"].(float64) != 2 || got.Meta["returned"].(float64) != 2 {
		t.Errorf("meta = %v", got.Meta)
	}
	if _, ok := got.Meta["nextRecordPosition"]; !ok {
		t.Error("nextRecordPosition 0 should still be present")
	}
	if got.Records[0]["title"] != "吾輩は猫である" || got.Records[0]["jpno"].([]any)[0] != "20570720" {
		t.Errorf("record = %v", got.Records[0])
	}
}

func TestJSONLFields(t *testing.T) {
	fields, err := NormalizeFields([]string{"title", "creator", "identifier"})
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := JSONL(&b, recs, fields); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d", len(lines))
	}
	want := `{"title":"吾輩は猫である","creators":["夏目, 漱石"],"identifier":"JP:20570720"}`
	if lines[0] != want {
		t.Errorf("got  %s\nwant %s", lines[0], want)
	}
}

func TestNormalizeFieldsRejectsUnknown(t *testing.T) {
	if _, err := NormalizeFields([]string{"titel"}); err == nil {
		t.Error("expected error")
	}
}

func TestTableFitsWidth(t *testing.T) {
	var b bytes.Buffer
	if err := Table(&b, recs, DefaultTableFields, 140); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %q", lines)
	}
	if !strings.HasPrefix(lines[0], "TITLE") || !strings.Contains(lines[0], "URL") {
		t.Errorf("header = %q", lines[0])
	}
	for _, l := range lines {
		if w := StringWidth(l); w > 140 {
			t.Errorf("line width %d > 140: %q", w, l)
		}
	}
	if !strings.Contains(lines[2], "…") {
		t.Errorf("long title should be truncated: %q", lines[2])
	}
	// URLs are never truncated.
	if !strings.Contains(lines[1], recs[0].URL) || !strings.Contains(lines[1], "JP:20570720") {
		t.Errorf("row = %q", lines[1])
	}
}

func TestWidth(t *testing.T) {
	if StringWidth("桜abc") != 5 {
		t.Error(StringWidth("桜abc"))
	}
	if got := Truncate("吾輩は猫である", 7); got != "吾輩は…" || StringWidth(got) > 7 {
		t.Errorf("Truncate = %q", got)
	}
}
