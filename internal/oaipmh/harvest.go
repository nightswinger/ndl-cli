package oaipmh

import (
	"context"
	"strings"

	"github.com/nightswinger/ndl-cli/internal/client"
)

// Page is one harvested response.
type Page struct {
	Resp *Response
	Raw  []byte
	// Token is the resumptionToken returned with this page ("" on the last page).
	Token            string
	CompleteListSize int
	Cursor           int
}

// HarvestOptions control Harvest.
type HarvestOptions struct {
	// Limit stops after this many items (<= 0: no limit). The last page is
	// truncated so exactly Limit items are delivered.
	Limit int
	// Follow continues with resumptionTokens; false fetches a single page.
	Follow bool
}

// HarvestResult summarizes a harvest.
type HarvestResult struct {
	Fetched int
	Pages   int
	// NextToken is the token to resume from. It is set when the harvest
	// stopped early (limit, Follow=false, error or cancellation) and more
	// data remains; resuming from it re-fetches the page in progress.
	NextToken string
}

// Harvest runs a list request (ListRecords, ListIdentifiers, ListSets) and
// follows resumptionTokens, passing each page to fn. The client enforces
// the request interval between pages.
func Harvest(ctx context.Context, c *client.Client, req Request, opt HarvestOptions, fn func(*Page) error) (HarvestResult, error) {
	var res HarvestResult
	res.NextToken = req.ResumptionToken
	for {
		resp, raw, err := Do(ctx, c, req)
		if err != nil {
			return res, err
		}
		page := &Page{Resp: resp, Raw: raw}
		if t := resp.Token(); t != nil {
			page.Token = strings.TrimSpace(t.Value)
			page.CompleteListSize = t.CompleteListSize
			page.Cursor = t.Cursor
		}
		n := resp.Len()
		if opt.Limit > 0 && res.Fetched+n > opt.Limit {
			resp.truncate(opt.Limit - res.Fetched)
			n = resp.Len()
		}
		res.Fetched += n
		res.Pages++
		if err := fn(page); err != nil {
			return res, err
		}
		res.NextToken = page.Token
		if page.Token == "" || !opt.Follow || (opt.Limit > 0 && res.Fetched >= opt.Limit) {
			return res, nil
		}
		req = Request{Verb: req.Verb, ResumptionToken: page.Token}
	}
}
