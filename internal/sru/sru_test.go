package sru

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nightswinger/ndl-cli/internal/client"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../../testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func testClient(url string) *client.Client {
	return client.New(client.Config{BaseURL: url, Interval: 0, Backoff: time.Millisecond, MaxRetries: 1})
}

func serve(t *testing.T, body []byte, check func(r *http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != Path {
			t.Errorf("path = %q", r.URL.Path)
		}
		if check != nil {
			check(r)
		}
		w.Header().Set("Content-Type", "text/xml;charset=UTF-8")
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCQL(t *testing.T) {
	tests := []struct {
		f       Fields
		want    string
		wantErr bool
	}{
		{Fields{Title: "桜", From: "2018"}, `title="桜" AND from="2018"`, false},
		{Fields{Creator: "夏目漱石"}, `creator="夏目漱石"`, false},
		{Fields{Title: "ruby python", DPID: []string{"iss-ndl-opac", "aozora"}}, `title="ruby python" AND dpid="iss-ndl-opac aozora"`, false},
		{Fields{Title: `say "hi"`}, `title="say \"hi\""`, false},
		{Fields{ISBN: "9784101010014", NDC: "913", MediaType: []string{"books"}}, `isbn="9784101010014" AND ndc="913" AND mediatype="books"`, false},
		{Fields{From: "2018-01", Until: "2019-12"}, `from="2018-01" AND until="2019-12"`, false},
		{Fields{From: "2018", Until: "2019-12"}, "", true},
		{Fields{From: "18"}, "", true},
		{Fields{}, "", true},
	}
	for _, tt := range tests {
		got, err := tt.f.CQL()
		if (err != nil) != tt.wantErr {
			t.Errorf("%+v: err = %v", tt.f, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%+v:\n got  %s\n want %s", tt.f, got, tt.want)
		}
	}
}

func TestRequestValues(t *testing.T) {
	r := Request{Query: `title="桜"`, StartRecord: 3, MaximumRecords: 20, RecordSchema: "dcndl", OnlyBib: true, InProcess: true}
	v := r.Values()
	for k, want := range map[string]string{
		"operation": "searchRetrieve", "query": `title="桜"`, "startRecord": "3",
		"maximumRecords": "20", "recordSchema": "dcndl", "onlyBib": "true", "inprocess": "true",
	} {
		if got := v.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if v.Has("recordPacking") {
		t.Error("recordPacking should be omitted when empty")
	}
}

func TestValidate(t *testing.T) {
	bad := []Request{
		{Query: ""},
		{Query: "x", MaximumRecords: 501},
		{Query: "x", RecordSchema: "dcndl_simple"},
		{Query: "x", RecordSchema: "oai_dc"},
		{Query: "x", RecordPacking: "json"},
		{Query: "x", RecordSchema: "dc", OnlyBib: true},
	}
	for _, r := range bad {
		if r.Validate() == nil {
			t.Errorf("%+v: expected error", r)
		}
	}
	if err := (Request{Query: "x", MaximumRecords: 500, RecordSchema: "dcndl_v3"}).Validate(); err != nil {
		t.Error(err)
	}
}

func TestSearchDCNDLXMLPacking(t *testing.T) {
	srv := serve(t, fixture(t, "sru_dcndl.xml"), func(r *http.Request) {
		if q := r.URL.Query().Get("query"); q != `title="桜" AND from="2018"` {
			t.Errorf("query = %q", q)
		}
	})
	resp, raw, err := Search(context.Background(), testClient(srv.URL), Request{Query: `title="桜" AND from="2018"`, MaximumRecords: 3, RecordSchema: "dcndl", RecordPacking: "xml"})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Error("raw body missing")
	}
	if resp.NumberOfRecords != 11625 || resp.NextRecordPosition != 4 || len(resp.Records) != 3 {
		t.Fatalf("got n=%d next=%d records=%d", resp.NumberOfRecords, resp.NextRecordPosition, len(resp.Records))
	}
	recs, err := resp.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if recs[1].Title == "" || recs[1].URL != "https://ndlsearch.ndl.go.jp/books/R000000004-I030071126" {
		t.Errorf("record 2 = %+v", recs[1])
	}
}

func TestSearchStringPacking(t *testing.T) {
	srv := serve(t, fixture(t, "sru_dcndl_string.xml"), nil)
	resp, _, err := Search(context.Background(), testClient(srv.URL), Request{Query: "x"})
	if err != nil {
		t.Fatal(err)
	}
	recs, err := resp.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("records = %d", len(recs))
	}
	for _, r := range recs {
		if r.Title == "" || len(r.JPNO) == 0 || !strings.HasPrefix(r.URL, "https://ndlsearch.ndl.go.jp/books/R100000002-") {
			t.Errorf("record = %+v", r)
		}
	}
}

func TestSearchDC(t *testing.T) {
	srv := serve(t, fixture(t, "sru_dc.xml"), nil)
	resp, _, err := Search(context.Background(), testClient(srv.URL), Request{Query: "x", RecordSchema: "dc"})
	if err != nil {
		t.Fatal(err)
	}
	recs, err := resp.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 || recs[0].Title == "" {
		t.Errorf("records = %+v", recs)
	}
}

func TestDiagnostics(t *testing.T) {
	srv := serve(t, fixture(t, "sru_diag_maxrecords.xml"), nil)
	_, _, err := Search(context.Background(), testClient(srv.URL), Request{Query: "x"})
	var de *DiagnosticsError
	if !errors.As(err, &de) {
		t.Fatalf("err = %v, want DiagnosticsError", err)
	}
	if de.Diagnostics[0].Message != "illegal maximumRecords value" {
		t.Errorf("message = %q", de.Diagnostics[0].Message)
	}
	if !strings.Contains(err.Error(), "illegal maximumRecords value") {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestDiagnosticsOnHTTPError(t *testing.T) {
	body := fixture(t, "sru_diag_maxrecords.xml")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write(body)
	}))
	defer srv.Close()
	_, _, err := Search(context.Background(), testClient(srv.URL), Request{Query: "x"})
	var de *DiagnosticsError
	if !errors.As(err, &de) {
		t.Fatalf("err = %v, want DiagnosticsError", err)
	}
}

func TestNoRecordIsNotAnError(t *testing.T) {
	srv := serve(t, fixture(t, "sru_diag_norecord.xml"), nil)
	resp, _, err := Search(context.Background(), testClient(srv.URL), Request{Query: "x"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !resp.Empty() || resp.NumberOfRecords != 0 {
		t.Errorf("resp = %+v", resp)
	}
}

// pagingServer simulates a result set of total records.
func pagingServer(t *testing.T, total int, calls *[]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		start, _ := strconv.Atoi(q.Get("startRecord"))
		max, _ := strconv.Atoi(q.Get("maximumRecords"))
		*calls = append(*calls, fmt.Sprintf("%d+%d", start, max))
		end := start + max - 1
		if end > total {
			end = total
		}
		next := end + 1
		if end >= total {
			next = 0
		}
		var b strings.Builder
		fmt.Fprintf(&b, `<searchRetrieveResponse xmlns="http://www.loc.gov/zing/srw/"><version>1.2</version><numberOfRecords>%d</numberOfRecords><nextRecordPosition>%d</nextRecordPosition><records>`, total, next)
		for i := start; i <= end; i++ {
			fmt.Fprintf(&b, `<record><recordSchema>dc</recordSchema><recordPacking>xml</recordPacking><recordData><srw_dc:dc xmlns:srw_dc="info:srw/schema/1/dc-v1.1" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>t%d</dc:title></srw_dc:dc></recordData><recordPosition>%d</recordPosition></record>`, i, i)
		}
		b.WriteString(`</records></searchRetrieveResponse>`)
		w.Write([]byte(b.String()))
	}))
}

func TestSearchAll(t *testing.T) {
	var calls []string
	srv := pagingServer(t, 25, &calls)
	defer srv.Close()

	var titles []string
	res, err := SearchAll(context.Background(), testClient(srv.URL), Request{Query: "x", MaximumRecords: 10}, 0, func(resp *Response, raw []byte) error {
		recs, err := resp.Decode()
		for _, r := range recs {
			titles = append(titles, r.Title)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Fetched != 25 || len(titles) != 25 || titles[24] != "t25" {
		t.Errorf("fetched=%d titles=%v", res.Fetched, titles)
	}
	if strings.Join(calls, ",") != "1+10,11+10,21+10" {
		t.Errorf("calls = %v", calls)
	}
}

func TestSearchAllLimit(t *testing.T) {
	var calls []string
	srv := pagingServer(t, 100, &calls)
	defer srv.Close()

	res, err := SearchAll(context.Background(), testClient(srv.URL), Request{Query: "x", MaximumRecords: 10}, 15, func(*Response, []byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if res.Fetched != 15 || strings.Join(calls, ",") != "1+10,11+5" {
		t.Errorf("fetched=%d calls=%v", res.Fetched, calls)
	}
}

func TestSearchAllWindow(t *testing.T) {
	var calls []string
	srv := pagingServer(t, 2000, &calls)
	defer srv.Close()

	res, err := SearchAll(context.Background(), testClient(srv.URL), Request{Query: "x", MaximumRecords: 200}, 0, func(*Response, []byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if res.Fetched != 500 || !res.WindowExhausted {
		t.Errorf("res = %+v", res)
	}
	if strings.Join(calls, ",") != "1+200,201+200,401+100" {
		t.Errorf("calls = %v", calls)
	}
}
