package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../../testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// routes maps "path?key=value" matchers to fixture files; the first match wins.
func fakeNDL(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(srv.Close)
	return srv.URL
}

func init() { retryBackoff = time.Millisecond }

func run(t *testing.T, base string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	all := append([]string{"--base-url", base, "--interval", "0"}, args...)
	code = Execute(context.Background(), all, &out, &errb)
	return code, out.String(), errb.String()
}

func TestSRUDryRun(t *testing.T) {
	code, out, _ := run(t, "https://ndlsearch.ndl.go.jp/api", "sru", `title="桜"`, "--dry-run")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	want := "https://ndlsearch.ndl.go.jp/api/sru?maximumRecords=20&operation=searchRetrieve&query=title%3D%22%E6%A1%9C%22&recordPacking=xml&recordSchema=dcndl&startRecord=1\n"
	if out != want {
		t.Errorf("got  %s\nwant %s", out, want)
	}
}

func TestSRUHelperFlags(t *testing.T) {
	code, out, _ := run(t, "http://x", "sru", "--title", "桜", "--from", "2018", "--dpid", "iss-ndl-opac,aozora", "--dry-run")
	if code != 0 || !strings.Contains(out, "query=title%3D%22%E6%A1%9C%22+AND+from%3D%222018%22+AND+dpid%3D%22iss-ndl-opac+aozora%22") {
		t.Errorf("code=%d out=%s", code, out)
	}
}

func TestUsageErrors(t *testing.T) {
	cases := [][]string{
		{"sru", `title="桜"`, "--title", "桜"}, // both CQL and helpers
		{"sru"},                              // neither
		{"sru", "x", "--schema", "dcndl_simple"},
		{"sru", "x", "--max", "501"},
		{"sru", "x", "--nope"},
		{"os", "--titel", "桜"}, // unknown flag rejected by cobra
		{"os", "--jpno", "123"},
		{"os", "--dpid", "aozora"},               // dpid alone
		{"oaipmh", "records", "--set", "aozora"}, // from is required
		{"oaipmh", "records", "--from", "2024-01-01", "--prefix", "dcndl_simple"},
		{"oaipmh", "get"},
		{"--format", "csv", "sru", "x"},
		{"--fields", "titel", "sru", "x"},
		{"nosuchcommand"},
	}
	for _, args := range cases {
		code, _, stderr := run(t, "http://127.0.0.1:1", args...)
		if code != ExitUsage {
			t.Errorf("%q: code = %d, want 2 (stderr: %s)", args, code, stderr)
		}
	}
}

func TestSRUJSON(t *testing.T) {
	base := fakeNDL(t, func(w http.ResponseWriter, r *http.Request) { w.Write(fixture(t, "sru_dcndl.xml")) })
	code, out, stderr := run(t, base, "sru", `title="桜" AND from="2018"`, "--max", "3", "--format", "json")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "numberOfRecords: 11625") {
		t.Errorf("stderr = %q", stderr)
	}
	var got struct {
		Meta    map[string]any   `json:"meta"`
		Records []map[string]any `json:"records"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Meta["numberOfRecords"].(float64) != 11625 || got.Meta["nextRecordPosition"].(float64) != 4 || len(got.Records) != 3 {
		t.Errorf("meta=%v records=%d", got.Meta, len(got.Records))
	}

	code, _, stderr = run(t, base, "--quiet", "sru", "x")
	if code != 0 || stderr != "" {
		t.Errorf("--quiet: code=%d stderr=%q", code, stderr)
	}
}

func TestSRUDiagnosticExit1(t *testing.T) {
	base := fakeNDL(t, func(w http.ResponseWriter, r *http.Request) { w.Write(fixture(t, "sru_diag_maxrecords.xml")) })
	code, out, stderr := run(t, base, "sru", "x")
	if code != ExitAPI || out != "" || !strings.Contains(stderr, "illegal maximumRecords value") {
		t.Errorf("code=%d out=%q stderr=%q", code, out, stderr)
	}
}

func TestSRUNoRecordExit0(t *testing.T) {
	base := fakeNDL(t, func(w http.ResponseWriter, r *http.Request) { w.Write(fixture(t, "sru_diag_norecord.xml")) })
	code, _, stderr := run(t, base, "sru", "x", "--format", "jsonl")
	if code != 0 || !strings.Contains(stderr, "numberOfRecords: 0") {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
}

func TestNetworkExit3(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	base := srv.URL
	srv.Close()
	code, _, stderr := run(t, base, "sru", "x")
	if code != ExitNetwork {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
}

func TestSRUAllXML(t *testing.T) {
	var n int
	base := fakeNDL(t, func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			w.Write(fixture(t, "sru_dcndl_book.xml")) // nextRecordPosition 3
			return
		}
		w.Write(fixture(t, "sru_dcndl_isbn.xml")) // last page
	})
	code, out, stderr := run(t, base, "sru", "x", "--all", "--max", "2", "--format", "xml")
	if code != 0 || n != 2 || strings.Count(out, "<searchRetrieveResponse") != 2 {
		t.Errorf("code=%d requests=%d stderr=%s", code, n, stderr)
	}
}

func TestOpenSearchTable(t *testing.T) {
	base := fakeNDL(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/opensearch" || r.URL.Query().Get("cnt") != "20" {
			t.Errorf("url = %s", r.URL)
		}
		w.Write(fixture(t, "os_title.xml"))
	})
	code, out, stderr := run(t, base, "os", "--title", "桜", "--from", "2018", "--cnt", "20")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "TITLE") || !strings.Contains(lines[1], "JP:23514280") ||
		!strings.Contains(lines[1], "https://ndlsearch.ndl.go.jp/books/R100000002-I031267586") {
		t.Errorf("table:\n%s", out)
	}
	if !strings.Contains(stderr, "totalResults: 4881") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestOpenSearchZeroWarning(t *testing.T) {
	base := fakeNDL(t, func(w http.ResponseWriter, r *http.Request) { w.Write(fixture(t, "os_zero.xml")) })
	code, _, stderr := run(t, base, "--quiet", "os", "--title", "zzqq")
	if code != 0 || !strings.Contains(stderr, "warning: 0 hits") || !strings.Contains(stderr, "typo") {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
}

func TestOAIIdentify(t *testing.T) {
	base := fakeNDL(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("verb") != "Identify" {
			t.Errorf("verb = %s", r.URL.Query().Get("verb"))
		}
		w.Write(fixture(t, "oai_identify.xml"))
	})
	code, out, _ := run(t, base, "harvest", "identify")
	if code != 0 || !strings.Contains(out, "国立国会図書館サーチ") {
		t.Errorf("code=%d out=%s", code, out)
	}
}

func TestOAIErrorExit1(t *testing.T) {
	base := fakeNDL(t, func(w http.ResponseWriter, r *http.Request) { w.Write(fixture(t, "oai_badtoken.xml")) })
	code, _, stderr := run(t, base, "oaipmh", "records", "--token", "bogus")
	if code != ExitAPI || !strings.Contains(stderr, "badResumptionToken") {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
}

func TestOAIRecordsOutFileAndResumeHint(t *testing.T) {
	var reqs []string
	base := fakeNDL(t, func(w http.ResponseWriter, r *http.Request) {
		reqs = append(reqs, r.URL.RawQuery)
		w.Write(fixture(t, "oai_records_dcndl.xml"))
	})
	out := filepath.Join(t.TempDir(), "h.jsonl")
	os.WriteFile(out, []byte(`{"existing":true}`+"\n"), 0o644)

	code, stdout, stderr := run(t, base, "harvest", "records", "--set", "aozora", "--from", "2026-01-01", "--follow=false", "--out", out)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if len(reqs) != 1 || !strings.Contains(reqs[0], "metadataPrefix=dcndl") || !strings.Contains(reqs[0], "set=aozora") {
		t.Errorf("requests = %v", reqs)
	}
	if stdout != "" {
		t.Errorf("stdout should be empty with --out, got %q", stdout)
	}
	b, _ := os.ReadFile(out)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 4 || !strings.Contains(lines[1], `"title":"あいびき"`) {
		t.Errorf("file:\n%s", b)
	}
	if !strings.Contains(stderr, "3/17840 items") || !strings.Contains(stderr, "--token 'dcndl/2026-01-01T00:00:00Z/") {
		t.Errorf("stderr = %s", stderr)
	}
}

func TestOAIInterruptPrintsToken(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var n int
	base := fakeNDL(t, func(w http.ResponseWriter, r *http.Request) {
		if n++; n == 2 {
			cancel() // Ctrl-C while page 2 is in flight
		}
		w.Write(fixture(t, "oai_identifiers.xml"))
	})
	var out, errb bytes.Buffer
	code := Execute(ctx, []string{"--base-url", base, "--interval", "0", "harvest", "identifiers", "--from", "2026-10-01", "--format", "jsonl"}, &out, &errb)
	if code != ExitInterrupted {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
	if strings.Count(out.String(), "\n") != 200 {
		t.Errorf("stdout lines = %d", strings.Count(out.String(), "\n"))
	}
	if !strings.Contains(errb.String(), "interrupted after 200 items. resume with:") ||
		!strings.Contains(errb.String(), "ndl oaipmh identifiers --token 'dcndl/2026-10-01T00:00:00Z/2026-10-01T23:59:59Z/iss-ndl-opac/200/") {
		t.Errorf("stderr = %s", errb.String())
	}
}

func TestOAIGetShortID(t *testing.T) {
	code, out, _ := run(t, "https://ndlsearch.ndl.go.jp/api", "oaipmh", "get", "R000000014-I000005card5000005", "--dry-run")
	if code != 0 || out != "https://ndlsearch.ndl.go.jp/api/oaipmh?identifier=oai%3Andlsearch.ndl.go.jp%3AR000000014-I000005card5000005&metadataPrefix=dcndl&verb=GetRecord\n" {
		t.Errorf("code=%d out=%s", code, out)
	}
}
