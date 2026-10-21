package dcndl

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// extract returns the first <open ...>...</close> fragment of a fixture.
func extract(t *testing.T, file, open, close string) []byte {
	t.Helper()
	b, err := os.ReadFile("../../testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	i := strings.Index(s, open)
	j := strings.Index(s, close)
	if i < 0 || j < 0 {
		t.Fatalf("%s: fragment %s not found", file, open)
	}
	return []byte(s[i : j+len(close)])
}

func TestParseRDFBook(t *testing.T) {
	r, err := Parse(extract(t, "sru_dcndl_isbn.xml", "<rdf:RDF", "</rdf:RDF>"))
	if err != nil {
		t.Fatal(err)
	}
	want := Record{
		ID:                 "R100000002-I000007305653",
		Title:              "吾輩は猫である",
		TitleTranscription: "ワガハイ ワ ネコ デ アル",
		Creators:           []string{"夏目, 漱石, 1867-1916"},
		Publishers:         []string{"新潮社"},
		Date:               "2003",
		ISBN:               []string{"4-10-101001-3"},
		JPNO:               []string{"20570720"},
		NDLBibID:           []string{"000007305653"},
		NDC:                []string{"913.6"},
		NDLC:               []string{"KH426"},
		MaterialType:       []string{"図書"},
		Language:           []string{"jpn"},
		URL:                "https://ndlsearch.ndl.go.jp/books/R100000002-I000007305653",
		SeeAlso: []string{
			"http://id.ndl.go.jp/jpno/20570720",
			"http://id.ndl.go.jp/bib/000007305653",
			"http://iss.ndl.go.jp/isbn/4101010013",
		},
	}
	if !reflect.DeepEqual(r, want) {
		t.Errorf("got  %+v\nwant %+v", r, want)
	}
	if got := r.Identifier(); got != "JP:20570720" {
		t.Errorf("Identifier() = %q", got)
	}
}

func TestParseRDFv3Article(t *testing.T) {
	r, err := Parse(extract(t, "sru_dcndl_v3.xml", "<rdf:RDF", "</rdf:RDF>"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.Title, "アーティストの表現として見る") {
		t.Errorf("title = %q", r.Title)
	}
	if !reflect.DeepEqual(r.Creators, []string{"成清, 北斗", "NARIKIYO, Hokuto"}) {
		t.Errorf("creators = %q", r.Creators)
	}
	if r.Date != "2022-03-25" {
		t.Errorf("date = %q", r.Date)
	}
	if len(r.Subjects) != 5 {
		t.Errorf("subjects (deduplicated) = %q", r.Subjects)
	}
	if r.ID != "R100000136-I1050855190174699136" {
		t.Errorf("id = %q", r.ID)
	}
}

func TestParseRDFFromOAI(t *testing.T) {
	r, err := Parse(extract(t, "oai_getrecord_dcndl.xml", "<rdf:RDF", "</rdf:RDF>"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "あいびき" || r.Date != "1969-12-25" || !reflect.DeepEqual(r.NDC, []string{"983"}) {
		t.Errorf("got %+v", r)
	}
	if !reflect.DeepEqual(r.Publishers, []string{"集英社"}) {
		t.Errorf("publishers = %q", r.Publishers)
	}
}

func TestParseOAIDC(t *testing.T) {
	r, err := Parse(extract(t, "oai_getrecord_oai_dc.xml", "<oai_dc:dc", "</oai_dc:dc>"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "あいびき" || !reflect.DeepEqual(r.Creators, []string{"ツルゲーネフ イワン 著者"}) ||
		!reflect.DeepEqual(r.Publishers, []string{"集英社"}) {
		t.Errorf("got %+v", r)
	}
}

func TestSetURL(t *testing.T) {
	var r Record
	r.SetURL("R100000002-I000007305653")
	if r.URL != "https://ndlsearch.ndl.go.jp/books/R100000002-I000007305653" || r.ID != "R100000002-I000007305653" {
		t.Errorf("got %+v", r)
	}
	if got := ItemFromOAIIdentifier("oai:ndlsearch.ndl.go.jp:R000000014-I000005card5000005"); got != "R000000014-I000005card5000005" {
		t.Errorf("ItemFromOAIIdentifier = %q", got)
	}
}
