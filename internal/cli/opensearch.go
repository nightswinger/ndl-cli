package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nightswinger/ndl/internal/opensearch"
	"github.com/nightswinger/ndl/internal/output"
)

func (a *app) openSearchCmd() *cobra.Command {
	var (
		p          opensearch.Params
		issn, jpno string
	)
	cmd := &cobra.Command{
		Use:     "opensearch",
		Aliases: []string{"os"},
		Short:   "Search with OpenSearch (RSS 2.0)",
		Long: `Search NDL Search with the OpenSearch API. Flags map 1:1 to the API
parameters (spec table 4-1). Conditions are AND-ed; space-separated words
within one flag are AND-ed, except dpid/mediatype which are OR-ed.

The API never reports parameter errors: bad values give 0 hits and unknown
parameter names are silently ignored. ndl warns about both.`,
		Example: `  ndl os --title 桜 --from 2018 --cnt 20
  ndl opensearch --creator 夏目 --dpid aozora --format json`,
		Args: noArgsUsage,
		RunE: func(cmd *cobra.Command, args []string) error {
			if issn != "" || jpno != "" {
				return usagef("the OpenSearch API has no issn/jpno parameter (the server would ignore it and match everything); use: ndl sru --issn/--jpno")
			}
			if err := p.Validate(); err != nil {
				return usageError{err}
			}
			c := a.client()
			if a.dryRun {
				fmt.Fprintln(a.stdout, c.URL(opensearch.Path, p.Values()))
				return nil
			}
			feed, raw, err := opensearch.Search(cmd.Context(), c, p)
			if err != nil {
				return err
			}
			a.info("totalResults: %d", feed.TotalResults)
			for _, w := range opensearch.Warnings(p, feed) {
				a.warn("%s", w)
			}
			if a.format == "xml" {
				return a.writeRaw(raw)
			}
			meta := output.Meta{Source: "opensearch", NumberOfRecords: output.Int(feed.TotalResults)}
			if next := feed.StartIndex + len(feed.Items); len(feed.Items) > 0 && next <= feed.TotalResults && next <= opensearch.MaxPosition {
				meta.NextRecordPosition = output.Int(next)
			}
			return output.Records(a.stdout, feed.Records(), meta, a.outputOptions())
		},
	}
	f := cmd.Flags()
	f.SetNormalizeFunc(renameFlags(map[string]string{"digitized-publisher": "digitized_publisher"}))
	f.StringVar(&p.Title, "title", "", "title (partial match)")
	f.StringVar(&p.Creator, "creator", "", "creator (partial match)")
	f.StringVar(&p.Publisher, "publisher", "", "publisher (partial match)")
	f.StringVar(&p.DigitizedPublisher, "digitized_publisher", "", "digitizing producer (partial match)")
	f.StringVar(&p.NDC, "ndc", "", "classification NDC/NDLC (prefix match)")
	f.StringVar(&p.From, "from", "", "publication date from (YYYY, YYYY-MM, YYYY-MM-DD)")
	f.StringVar(&p.Until, "until", "", "publication date until (same format as --from)")
	f.StringVar(&p.ISBN, "isbn", "", "ISBN (10/13 digits: exact; otherwise prefix match)")
	f.StringSliceVar(&p.MediaType, "mediatype", nil, "mediatype values (comma-separated, OR)")
	f.StringSliceVar(&p.DPID, "dpid", nil, "data provider / data group IDs (comma-separated, OR)")
	f.StringVar(&p.DPGroupID, "dpgroupid", "", "data provider group ID (discontinued Dec 2023)")
	f.StringVar(&p.Any, "any", "", "any field (same as NDL Search simple search)")
	f.IntVar(&p.Cnt, "cnt", 20, fmt.Sprintf("records per response (1-%d)", opensearch.MaxCnt))
	f.IntVar(&p.Idx, "idx", 1, "start position (1-based)")
	f.StringVar(&issn, "issn", "", "not supported by OpenSearch; use 'ndl sru --issn'")
	f.StringVar(&jpno, "jpno", "", "not supported by OpenSearch; use 'ndl sru --jpno'")
	_ = f.MarkHidden("issn")
	_ = f.MarkHidden("jpno")
	return cmd
}
