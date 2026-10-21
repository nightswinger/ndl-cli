package opensearch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
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

func serve(t *testing.T, status int, body []byte, check func(*http.Request)) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != Path {
			t.Errorf("path = %q", r.URL.Path)
		}
		if check != nil {
			check(r)
		}
		w.WriteHeader(status)
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return client.New(client.Config{BaseURL: srv.URL, Backoff: time.Millisecond, MaxRetries: 1})
}

func TestValues(t *testing.T) {
	p := Params{Title: "桜", From: "2018", Cnt: 20, DPID: []string{"iss-ndl-opac", "aozora"}, MediaType: []string{"books"}}
	v := p.Values()
	want := map[string]string{"title": "桜", "from": "2018", "cnt": "20", "dpid": "iss-ndl-opac aozora", "mediatype": "books"}
	for k, w := range want {
		if v.Get(k) != w {
			t.Errorf("%s = %q, want %q", k, v.Get(k), w)
		}
	}
	if len(v) != len(want) {
		t.Errorf("unexpected params: %v", v)
	}
}

func TestValidate(t *testing.T) {
	if err := (Params{DPID: []string{"aozora"}}).Validate(); err == nil {
		t.Error("dpid-only search should be rejected")
	}
	if err := (Params{Title: "x", Cnt: 600}).Validate(); err == nil {
		t.Error("cnt 600 should be rejected")
	}
	if err := (Params{Title: "x", From: "2018", Until: "2019-01"}).Validate(); err == nil {
		t.Error("mixed date formats should be rejected")
	}
	if err := (Params{Title: "x", Cnt: 500}).Validate(); err != nil {
		t.Error(err)
	}
}

func TestSearch(t *testing.T) {
	c := serve(t, 200, fixture(t, "os_title.xml"), func(r *http.Request) {
		if r.URL.Query().Get("title") != "桜" {
			t.Errorf("title = %q", r.URL.Query().Get("title"))
		}
	})
	f, raw, err := Search(context.Background(), c, Params{Title: "桜", From: "2018", Cnt: 2, DPID: []string{"iss-ndl-opac"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || f.TotalResults != 4881 || f.ItemsPerPage != 2 || len(f.Items) != 2 {
		t.Fatalf("feed = %+v", f)
	}
	r := f.Records()[0]
	if r.Title != "アーモンドアイ : 9冠女王、最強の耀き" || r.ID != "R100000002-I031267586" ||
		r.URL != "https://ndlsearch.ndl.go.jp/books/R100000002-I031267586" || r.Date != "2021" ||
		!reflect.DeepEqual(r.JPNO, []string{"23514280"}) || !reflect.DeepEqual(r.NDLC, []string{"YL321"}) ||
		!reflect.DeepEqual(r.Publishers, []string{"フジテレビジョン", "ポニーキャニオン"}) {
		t.Errorf("record = %+v", r)
	}
	if w := Warnings(Params{Title: "桜"}, f); len(w) != 0 {
		t.Errorf("unexpected warnings %v", w)
	}
}

func TestISBNItem(t *testing.T) {
	c := serve(t, 200, fixture(t, "os_isbn.xml"), nil)
	f, _, err := Search(context.Background(), c, Params{ISBN: "9784101010014"})
	if err != nil {
		t.Fatal(err)
	}
	r := f.Records()[0]
	if !reflect.DeepEqual(r.ISBN, []string{"9784101010014"}) || !reflect.DeepEqual(r.NDC, []string{"913.6"}) ||
		!reflect.DeepEqual(r.Creators, []string{"夏目, 漱石"}) {
		t.Errorf("record = %+v", r)
	}
	if got := r.Identifier(); got != "ISBN:9784101010014" {
		t.Errorf("Identifier = %q", got)
	}
}

func TestZeroHitsWarning(t *testing.T) {
	for _, name := range []string{"os_zero.xml", "os_cnt600.xml"} {
		c := serve(t, 200, fixture(t, name), nil)
		p := Params{Title: "zzqqxxyyqqzz9876"}
		f, _, err := Search(context.Background(), c, p)
		if err != nil {
			t.Fatal(err)
		}
		w := Warnings(p, f)
		if f.TotalResults != 0 || len(w) != 1 || !strings.Contains(w[0], "typo") {
			t.Errorf("%s: total=%d warnings=%v", name, f.TotalResults, w)
		}
	}
}

func TestIgnoredParamsWarning(t *testing.T) {
	// The server drops unknown parameters such as "titel" and searches everything.
	c := serve(t, 200, fixture(t, "os_typo.xml"), nil)
	p := Params{Title: "桜"}
	f, _, err := Search(context.Background(), c, p)
	if err != nil {
		t.Fatal(err)
	}
	if w := Warnings(p, f); len(w) != 1 || !strings.Contains(w[0], "ignored") {
		t.Errorf("warnings = %v", w)
	}
}

func TestErrorBody(t *testing.T) {
	_, err := Parse(fixture(t, "error_429.xml"))
	var ae *APIError
	if !errors.As(err, &ae) || ae.Code != "429" {
		t.Fatalf("err = %v", err)
	}
}
