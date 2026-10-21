package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nightswinger/ndl/internal/dcndl"
	"github.com/nightswinger/ndl/internal/output"
	"github.com/nightswinger/ndl/internal/sru"
)

func (a *app) sruCmd() *cobra.Command {
	var (
		req    sru.Request
		fields sru.Fields
		all    bool
		limit  int
	)
	cmd := &cobra.Command{
		Use:   "sru [CQL query]",
		Short: "Search with SRU (searchRetrieve, CQL)",
		Long: `Search NDL Search with SRU. Pass a CQL query, or build one with the
helper flags (--title, --creator, ...). Helper flags are AND-ed.

Supported recordSchema values are dcndl, dcndl_v3 and dc. The server returns
at most 500 records per request and never beyond the 500th record, so --all
stops there.`,
		Example: `  ndl sru 'title="桜" AND from="2018"' --max 20
  ndl sru --title 桜 --from 2018 --dpid iss-ndl-opac
  ndl sru 'creator="夏目漱石"' --all --limit 300 --format jsonl`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case len(args) == 1 && !fields.Empty():
				return usagef("give either a CQL query or helper flags (--title etc.), not both")
			case len(args) == 1:
				req.Query = args[0]
			case !fields.Empty():
				q, err := fields.CQL()
				if err != nil {
					return usageError{err}
				}
				req.Query = q
			default:
				return usagef("a CQL query or at least one helper flag (--title, --creator, ...) is required")
			}
			if err := req.Validate(); err != nil {
				return usageError{err}
			}
			if limit < 0 {
				return usagef("--limit must be >= 0")
			}
			if limit > 0 && !all {
				return usagef("--limit is only meaningful with --all (use --max for a single page)")
			}
			c := a.client()
			if a.dryRun {
				fmt.Fprintln(a.stdout, c.URL(sru.Path, req.Values()))
				return nil
			}
			ctx := cmd.Context()

			if !all {
				resp, raw, err := sru.Search(ctx, c, req)
				if err != nil {
					return err
				}
				a.info("numberOfRecords: %d", resp.NumberOfRecords)
				if a.format == "xml" {
					return a.writeRaw(raw)
				}
				recs, err := resp.Decode()
				if err != nil {
					return err
				}
				return output.Records(a.stdout, recs, a.sruMeta(req.Query, resp.NumberOfRecords, resp.NextRecordPosition), a.outputOptions())
			}

			var acc []dcndl.Record
			res, err := sru.SearchAll(ctx, c, req, limit, func(resp *sru.Response, raw []byte) error {
				switch a.format {
				case "xml":
					return a.writeRaw(raw)
				case "jsonl":
					recs, err := resp.Decode()
					if err != nil {
						return err
					}
					return output.JSONL(a.stdout, recs, a.outFields)
				}
				recs, err := resp.Decode()
				acc = append(acc, recs...)
				return err
			})
			if err == nil || len(acc) > 0 {
				if a.format == "table" || a.format == "json" {
					if werr := output.Records(a.stdout, acc, a.sruMeta(req.Query, res.NumberOfRecords, res.NextRecordPosition), a.outputOptions()); werr != nil && err == nil {
						err = werr
					}
				}
			}
			a.info("numberOfRecords: %d (fetched %d)", res.NumberOfRecords, res.Fetched)
			if res.WindowExhausted {
				a.warn("SRU cannot return records beyond position %d; narrow the query (e.g. --from/--until) to get the rest", sru.MaxPosition)
			}
			return err
		},
	}
	f := cmd.Flags()
	f.IntVar(&req.StartRecord, "start", 1, "startRecord (1-based)")
	f.IntVar(&req.MaximumRecords, "max", 20, fmt.Sprintf("maximumRecords per request (1-%d)", sru.MaxRecords))
	f.StringVar(&req.RecordSchema, "schema", "dcndl", "recordSchema: "+strings.Join(sru.Schemas, "|"))
	f.StringVar(&req.RecordPacking, "packing", "xml", "recordPacking: xml|string")
	f.BoolVar(&req.OnlyBib, "only-bib", false, "return only dcndl:BibResource (onlyBib=true; dcndl/dcndl_v3 only)")
	f.BoolVar(&req.InProcess, "inprocess", false, "search only NDL in-process (新着書誌) records (inprocess=true)")
	f.BoolVar(&all, "all", false, "follow nextRecordPosition and fetch every page (max 500 records)")
	f.IntVar(&limit, "limit", 0, "stop --all after this many records (0 = no limit)")

	f.StringVar(&fields.Title, "title", "", "CQL helper: title")
	f.StringVar(&fields.Creator, "creator", "", "CQL helper: creator")
	f.StringVar(&fields.Publisher, "publisher", "", "CQL helper: publisher")
	f.StringVar(&fields.Anywhere, "anywhere", "", "CQL helper: anywhere (same fields as NDL Search simple search)")
	f.StringVar(&fields.ISBN, "isbn", "", "CQL helper: isbn (exact match, 10 or 13 digits)")
	f.StringVar(&fields.ISSN, "issn", "", "CQL helper: issn")
	f.StringVar(&fields.JPNO, "jpno", "", "CQL helper: jpno (JP number)")
	f.StringVar(&fields.NDC, "ndc", "", "CQL helper: ndc (prefix match)")
	f.StringVar(&fields.From, "from", "", "CQL helper: from (YYYY, YYYY-MM or YYYY-MM-DD)")
	f.StringVar(&fields.Until, "until", "", "CQL helper: until (same format as --from)")
	f.StringSliceVar(&fields.DPID, "dpid", nil, "CQL helper: data provider / data group IDs (comma-separated, OR)")
	f.StringSliceVar(&fields.MediaType, "mediatype", nil, "CQL helper: mediatype values (comma-separated, OR)")
	return cmd
}

func (a *app) sruMeta(query string, n, next int) output.Meta {
	return output.Meta{Source: "sru", Query: query, NumberOfRecords: output.Int(n), NextRecordPosition: output.Int(next)}
}
