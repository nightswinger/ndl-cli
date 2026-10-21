package oaipmh

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nightswinger/ndl/internal/client"
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
	return client.New(client.Config{BaseURL: url, Backoff: time.Millisecond, MaxRetries: 1})
}

func serveFixture(t *testing.T, name string) *client.Client {
	body := fixture(t, name)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	t.Cleanup(srv.Close)
	return testClient(srv.URL)
}

func TestValues(t *testing.T) {
	r := Request{Verb: VerbListRecords, MetadataPrefix: "dcndl", From: "2024-01-01", Set: "aozora"}
	if got := r.Values().Encode(); got != "from=2024-01-01&metadataPrefix=dcndl&set=aozora&verb=ListRecords" {
		t.Errorf("got %s", got)
	}
	r.ResumptionToken = "dcndl/2026/x,y"
	if got := r.Values().Encode(); got != "resumptionToken=dcndl%2F2026%2Fx%2Cy&verb=ListRecords" {
		t.Errorf("token must be exclusive, got %s", got)
	}
}

func TestValidate(t *testing.T) {
	bad := []Request{
		{Verb: VerbListRecords, MetadataPrefix: "dcndl"},                            // no from
		{Verb: VerbListRecords, MetadataPrefix: "dcndl_simple", From: "2024-01-01"}, // bad prefix
		{Verb: VerbListIdentifiers, MetadataPrefix: "dcndl", From: "2024/01/01"},    // bad date
		{Verb: VerbGetRecord, MetadataPrefix: "dcndl"},                              // no identifier
	}
	for _, r := range bad {
		if r.Validate() == nil {
			t.Errorf("%+v: expected error", r)
		}
	}
	ok := []Request{
		{Verb: VerbListRecords, MetadataPrefix: "dcndl_v3", From: "2024-01-01T00:00:00Z"},
		{Verb: VerbListRecords, ResumptionToken: "x"},
		{Verb: VerbIdentify},
	}
	for _, r := range ok {
		if err := r.Validate(); err != nil {
			t.Errorf("%+v: %v", r, err)
		}
	}
}

func TestIdentify(t *testing.T) {
	resp, _, err := Do(context.Background(), serveFixture(t, "oai_identify.xml"), Request{Verb: VerbIdentify})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Identify == nil || resp.Identify.RepositoryName != "国立国会図書館サーチ" || resp.Identify.Granularity != "YYYY-MM-DDThh:mm:ssZ" {
		t.Errorf("identify = %+v", resp.Identify)
	}
}

func TestFormatsAndSets(t *testing.T) {
	resp, _, err := Do(context.Background(), serveFixture(t, "oai_formats.xml"), Request{Verb: VerbListMetadataFormats})
	if err != nil {
		t.Fatal(err)
	}
	var prefixes []string
	for _, f := range resp.Formats {
		prefixes = append(prefixes, f.Prefix)
	}
	if strings.Join(prefixes, ",") != "oai_dc,dcndl,dcndl_v3" {
		t.Errorf("prefixes = %v", prefixes)
	}

	resp, _, err = Do(context.Background(), serveFixture(t, "oai_sets.xml"), Request{Verb: VerbListSets})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Sets) < 10 || resp.Sets[0].Spec != "A00000" || resp.Sets[0].Name != "デジタル化資料" {
		t.Errorf("sets = %v", resp.Sets[:3])
	}
}

func TestListRecordsDCNDL(t *testing.T) {
	resp, _, err := Do(context.Background(), serveFixture(t, "oai_records_dcndl.xml"), Request{Verb: VerbListRecords, MetadataPrefix: "dcndl", From: "2026-01-01", Set: "aozora"})
	if err != nil {
		t.Fatal(err)
	}
	tok := resp.Token()
	if tok == nil || tok.CompleteListSize != 17840 || !strings.HasPrefix(tok.Value, "dcndl/2026-01-01") {
		t.Fatalf("token = %+v", tok)
	}
	recs, err := resp.Common()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("records = %d", len(recs))
	}
	r := recs[0]
	if r.Title != "あいびき" || r.OAIIdentifier != "oai:ndlsearch.ndl.go.jp:R000000014-I000005card5000005" ||
		r.ID != "R000000014-I000005card5000005" || r.Datestamp == "" || len(r.Sets) == 0 {
		t.Errorf("record = %+v", r)
	}
}

func TestGetRecordOAIDC(t *testing.T) {
	resp, _, err := Do(context.Background(), serveFixture(t, "oai_getrecord_oai_dc.xml"), Request{Verb: VerbGetRecord, MetadataPrefix: "oai_dc", Identifier: "x"})
	if err != nil {
		t.Fatal(err)
	}
	recs, err := resp.Common()
	if err != nil || len(recs) != 1 || recs[0].Title != "あいびき" || recs[0].URL == "" {
		t.Fatalf("recs = %+v err = %v", recs, err)
	}
}

func TestErrors(t *testing.T) {
	cases := map[string]string{
		"oai_nofrom.xml":    "badArgument",
		"oai_badprefix.xml": "cannotDisseminateFormat",
		"oai_badtoken.xml":  "badResumptionToken",
	}
	for file, code := range cases {
		_, _, err := Do(context.Background(), serveFixture(t, file), Request{Verb: VerbListRecords})
		var el ErrorList
		if !errors.As(err, &el) || el[0].Code != code {
			t.Errorf("%s: err = %v, want %s", file, err, code)
		}
	}
}

func TestNoRecordsMatchIsEmpty(t *testing.T) {
	resp, _, err := Do(context.Background(), serveFixture(t, "oai_norecords.xml"), Request{Verb: VerbListIdentifiers})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.NoRecords() || resp.Len() != 0 {
		t.Errorf("resp = %+v", resp)
	}
}

// Real page 1 and page 2 of a ListIdentifiers harvest, then a synthetic final page.
func tokenServer(t *testing.T, requests *[]string) *client.Client {
	p1, p2 := fixture(t, "oai_identifiers.xml"), fixture(t, "oai_identifiers_p2.xml")
	tok2 := "dcndl/2026-10-01T00:00:00Z/2026-10-01T23:59:59Z/iss-ndl-opac/400/1790837793229_R100000002-I000000096416-i10071670,1790837998080_R100000002-I034898234"
	last := `<?xml version="1.0" encoding="UTF-8"?><OAI-PMH xmlns="http://www.openarchives.org/OAI/2.0/"><ListIdentifiers>
<header><identifier>oai:ndlsearch.ndl.go.jp:R100000002-I1</identifier><datestamp>2026-10-01T10:00:00Z</datestamp><setSpec>iss-ndl-opac</setSpec></header>
<header status="deleted"><identifier>oai:ndlsearch.ndl.go.jp:R100000002-I2</identifier><datestamp>2026-10-01T10:00:00Z</datestamp></header>
<resumptionToken completeListSize="402" cursor="400"/></ListIdentifiers></OAI-PMH>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		*requests = append(*requests, q.Encode())
		switch tok := q.Get("resumptionToken"); {
		case tok == "":
			w.Write(p1)
		case strings.Contains(tok, "/200/"):
			if len(q) != 2 {
				t.Errorf("token request must only carry verb+token: %v", q)
			}
			w.Write(p2)
		case tok == tok2:
			w.Write([]byte(last))
		default:
			t.Errorf("unexpected token %q", tok)
		}
	}))
	t.Cleanup(srv.Close)
	return testClient(srv.URL)
}

func TestHarvestFollowsTokens(t *testing.T) {
	var reqs []string
	c := tokenServer(t, &reqs)
	var ids []string
	var deleted int
	res, err := Harvest(context.Background(), c,
		Request{Verb: VerbListIdentifiers, MetadataPrefix: "dcndl", Set: "iss-ndl-opac", From: "2026-10-01", Until: "2026-10-01"},
		HarvestOptions{Follow: true}, func(p *Page) error {
			recs, err := p.Resp.Common()
			for _, r := range recs {
				ids = append(ids, r.OAIIdentifier)
				if r.Deleted {
					deleted++
				}
			}
			return err
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 3 || res.Pages != 3 || res.Fetched != 402 || len(ids) != 402 || deleted != 1 {
		t.Errorf("reqs=%d res=%+v ids=%d deleted=%d", len(reqs), res, len(ids), deleted)
	}
	if res.NextToken != "" {
		t.Errorf("NextToken = %q, want empty after the last page", res.NextToken)
	}
}

func TestHarvestLimit(t *testing.T) {
	var reqs []string
	c := tokenServer(t, &reqs)
	var n int
	res, err := Harvest(context.Background(), c,
		Request{Verb: VerbListIdentifiers, MetadataPrefix: "dcndl", From: "2026-10-01"},
		HarvestOptions{Follow: true, Limit: 250}, func(p *Page) error { n += p.Resp.Len(); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 2 || n != 250 || res.Fetched != 250 {
		t.Errorf("reqs=%d n=%d res=%+v", len(reqs), n, res)
	}
	if !strings.Contains(res.NextToken, "/400/") {
		t.Errorf("NextToken = %q", res.NextToken)
	}
}

func TestHarvestNoFollow(t *testing.T) {
	var reqs []string
	c := tokenServer(t, &reqs)
	res, err := Harvest(context.Background(), c,
		Request{Verb: VerbListIdentifiers, MetadataPrefix: "dcndl", From: "2026-10-01"},
		HarvestOptions{Follow: false}, func(*Page) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 || res.Fetched != 200 || !strings.Contains(res.NextToken, "/200/") {
		t.Errorf("reqs=%d res=%+v", len(reqs), res)
	}
}

func TestHarvestCancelKeepsToken(t *testing.T) {
	var reqs []string
	c := tokenServer(t, &reqs)
	ctx, cancel := context.WithCancel(context.Background())
	res, err := Harvest(ctx, c,
		Request{Verb: VerbListIdentifiers, MetadataPrefix: "dcndl", From: "2026-10-01"},
		HarvestOptions{Follow: true}, func(p *Page) error {
			cancel() // SIGINT arrives while page 1 is being processed
			return nil
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if res.Fetched != 200 || !strings.Contains(res.NextToken, "/200/") {
		t.Errorf("res = %+v", res)
	}
}

func TestHarvestErrorMidway(t *testing.T) {
	p1 := fixture(t, "oai_identifiers.xml")
	bad := fixture(t, "oai_badtoken.xml")
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			w.Write(p1)
			return
		}
		w.Write(bad)
	}))
	defer srv.Close()
	res, err := Harvest(context.Background(), testClient(srv.URL), Request{Verb: VerbListIdentifiers, MetadataPrefix: "dcndl", From: "2026-10-01"},
		HarvestOptions{Follow: true}, func(*Page) error { return nil })
	var el ErrorList
	if !errors.As(err, &el) || el[0].Code != "badResumptionToken" {
		t.Fatalf("err = %v", err)
	}
	if res.Fetched != 200 || res.NextToken == "" {
		t.Errorf("res = %+v", res)
	}
}
