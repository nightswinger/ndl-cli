package sru

import (
	"context"

	"github.com/nightswinger/ndl-cli/internal/client"
)

// PageFunc receives each fetched page. Returning an error stops paging.
type PageFunc func(resp *Response, raw []byte) error

// PageResult summarizes a paged search.
type PageResult struct {
	NumberOfRecords int
	Fetched         int
	// NextRecordPosition is where the next page would start (0 if none).
	NextRecordPosition int
	// WindowExhausted is set when more records exist but lie beyond the
	// server's 500-record retrieval window.
	WindowExhausted bool
}

// SearchAll pages through results starting at req.StartRecord, using
// req.MaximumRecords as the page size, until there is no next page, the
// 500-record window is exhausted, or limit records were delivered
// (limit <= 0 means no limit). The client enforces the request interval.
func SearchAll(ctx context.Context, c *client.Client, req Request, limit int, fn PageFunc) (PageResult, error) {
	var res PageResult
	if req.StartRecord <= 0 {
		req.StartRecord = 1
	}
	if req.MaximumRecords <= 0 {
		req.MaximumRecords = 200
	}
	pageSize := req.MaximumRecords
	for {
		size := pageSize
		if limit > 0 && limit-res.Fetched < size {
			size = limit - res.Fetched
		}
		if last := req.StartRecord + size - 1; last > MaxPosition {
			size = MaxPosition - req.StartRecord + 1
		}
		if size <= 0 {
			break
		}
		req.MaximumRecords = size

		resp, raw, err := Search(ctx, c, req)
		if err != nil {
			return res, err
		}
		res.NumberOfRecords = resp.NumberOfRecords
		res.NextRecordPosition = resp.NextRecordPosition
		res.Fetched += len(resp.Records)
		if err := fn(resp, raw); err != nil {
			return res, err
		}
		if resp.NextRecordPosition <= 0 || len(resp.Records) == 0 {
			res.NextRecordPosition = 0
			break
		}
		if limit > 0 && res.Fetched >= limit {
			break
		}
		if resp.NextRecordPosition > MaxPosition {
			res.WindowExhausted = true
			break
		}
		req.StartRecord = resp.NextRecordPosition
	}
	return res, nil
}
