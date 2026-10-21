//go:build integration

// Live tests against the real NDL Search API. Run with:
//
//	go test -tags integration ./internal/cli/
//
// They send a handful of requests at a 2s interval; keep it that way.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nightswinger/ndl-cli/internal/client"
)

func live(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	time.Sleep(2 * time.Second) // be gentle with the shared service
	var out, errb bytes.Buffer
	all := append([]string{"--base-url", client.DefaultBaseURL, "--interval", "2s"}, args...)
	code := Execute(context.Background(), all, &out, &errb)
	return code, out.String(), errb.String()
}

func TestLiveSRU(t *testing.T) {
	code, out, stderr := live(t, "sru", `title="桜" AND from="2018"`, "--max", "3", "--format", "json")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	var got struct {
		Meta    map[string]any   `json:"meta"`
		Records []map[string]any `json:"records"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Records) != 3 || got.Meta["numberOfRecords"].(float64) < 1000 {
		t.Errorf("meta=%v records=%d", got.Meta, len(got.Records))
	}
}

func TestLiveSRUDiagnostic(t *testing.T) {
	code, _, stderr := live(t, "sru", `title="桜" AND mediatype="nosuchtype"`, "--max", "1")
	if code != ExitAPI || !strings.Contains(stderr, "illegal mediaType value") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
}

func TestLiveOpenSearch(t *testing.T) {
	code, out, stderr := live(t, "os", "--isbn", "9784101010014", "--cnt", "2", "--format", "jsonl")
	if code != 0 || !strings.Contains(out, "吾輩は猫である") {
		t.Errorf("code=%d out=%s stderr=%s", code, out, stderr)
	}
}

func TestLiveOAIIdentify(t *testing.T) {
	code, out, stderr := live(t, "oaipmh", "identify", "--format", "json")
	if code != 0 || !strings.Contains(out, "国立国会図書館サーチ") {
		t.Errorf("code=%d out=%s stderr=%s", code, out, stderr)
	}
}

func TestLiveOAIIdentifiersOnePage(t *testing.T) {
	from := time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	code, out, stderr := live(t, "oaipmh", "identifiers", "--set", "iss-ndl-opac", "--from", from, "--until", from, "--follow=false", "--limit", "5", "--format", "jsonl")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if n := strings.Count(out, "\n"); n > 5 {
		t.Errorf("lines = %d, want <= 5", n)
	}
}
