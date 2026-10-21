package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nightswinger/ndl/internal/dcndl"
	"github.com/nightswinger/ndl/internal/oaipmh"
	"github.com/nightswinger/ndl/internal/output"
)

// oaiIDPrefix is prepended to bare item tokens given to "get".
const oaiIDPrefix = "oai:ndlsearch.ndl.go.jp:"

type oaiOpts struct {
	prefix, from, until, set, token string
	limit                           int
	follow                          bool
	out                             string
}

func (a *app) oaipmhCmd() *cobra.Command {
	o := &oaiOpts{}
	cmd := &cobra.Command{
		Use:     "oaipmh",
		Aliases: []string{"harvest"},
		Short:   "Harvest metadata with OAI-PMH",
		Long: `Harvest NDL Search metadata with OAI-PMH 2.0.

metadataPrefix is one of dcndl, dcndl_v3, oai_dc. ListRecords and
ListIdentifiers require --from (the range may span at most one year; without
--until the server assumes from + 1 year). Pages hold 200 items and are
followed via resumptionToken; press Ctrl-C to stop and get a resume token.`,
		Args: noArgsUsage,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	pf := cmd.PersistentFlags()
	pf.StringVar(&o.prefix, "prefix", "dcndl", "metadataPrefix: "+strings.Join(oaipmh.Prefixes, "|"))
	pf.StringVar(&o.from, "from", "", "lower datestamp bound (YYYY-MM-DD or YYYY-MM-DDThh:mm:ssZ, JST)")
	pf.StringVar(&o.until, "until", "", "upper datestamp bound (same format as --from)")
	pf.StringVar(&o.set, "set", "", "set: data provider ID, data group ID, collection code or access rights")
	pf.StringVar(&o.token, "token", "", "resume from this resumptionToken (other selection flags are ignored)")

	cmd.AddCommand(
		a.oaiSimpleCmd("identify", "Show repository information (Identify)", oaipmh.VerbIdentify, o),
		a.oaiSimpleCmd("formats", "List metadata formats (ListMetadataFormats)", oaipmh.VerbListMetadataFormats, o),
		a.oaiListCmd("sets", "List sets (ListSets)", oaipmh.VerbListSets, o),
		a.oaiListCmd("identifiers", "List record headers (ListIdentifiers)", oaipmh.VerbListIdentifiers, o),
		a.oaiListCmd("records", "Harvest records (ListRecords)", oaipmh.VerbListRecords, o),
		a.oaiGetCmd(o),
	)
	return cmd
}

func (a *app) oaiSimpleCmd(use, short, verb string, o *oaiOpts) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  noArgsUsage,
		RunE: func(cmd *cobra.Command, args []string) error {
			req := oaipmh.Request{Verb: verb}
			c := a.client()
			if a.dryRun {
				fmt.Fprintln(a.stdout, c.URL(oaipmh.Path, req.Values()))
				return nil
			}
			resp, raw, err := oaipmh.Do(cmd.Context(), c, req)
			if err != nil {
				return err
			}
			if a.format == "xml" {
				return a.writeRaw(raw)
			}
			if verb == oaipmh.VerbIdentify {
				id := resp.Identify
				if id == nil {
					return errors.New("Identify response has no <Identify> element")
				}
				if a.format == "table" {
					return output.Grid(a.stdout, []string{"KEY", "VALUE"}, [][]string{
						{"repositoryName", id.RepositoryName},
						{"baseURL", id.BaseURL},
						{"protocolVersion", id.ProtocolVersion},
						{"earliestDatestamp", id.EarliestDatestamp},
						{"deletedRecord", id.DeletedRecord},
						{"granularity", id.Granularity},
						{"adminEmail", strings.Join(id.AdminEmail, ", ")},
					}, nil, 0)
				}
				return a.writeJSONValue(id)
			}
			switch a.format {
			case "table":
				rows := make([][]string, len(resp.Formats))
				for i, f := range resp.Formats {
					rows[i] = []string{f.Prefix, f.Namespace, f.Schema}
				}
				return output.Grid(a.stdout, []string{"PREFIX", "NAMESPACE", "SCHEMA"}, rows, nil, 0)
			case "jsonl":
				for _, f := range resp.Formats {
					if err := a.writeJSONLine(a.stdout, f); err != nil {
						return err
					}
				}
				return nil
			}
			return a.writeJSONValue(resp.Formats)
		},
	}
}

func (a *app) oaiGetCmd(o *oaiOpts) *cobra.Command {
	return &cobra.Command{
		Use:   "get <identifier>",
		Short: "Fetch one record (GetRecord)",
		Long: `Fetch one record by OAI identifier, e.g.
oai:ndlsearch.ndl.go.jp:R100000002-I000001287827. A bare item token
(R100000002-I000001287827) is accepted and prefixed automatically.`,
		Example: "  ndl oaipmh get R000000014-I000005card5000005 --prefix dcndl --format json",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usagef("get requires exactly one identifier")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if !strings.HasPrefix(id, "oai:") {
				id = oaiIDPrefix + id
			}
			req := oaipmh.Request{Verb: oaipmh.VerbGetRecord, Identifier: id, MetadataPrefix: o.prefix}
			if err := req.Validate(); err != nil {
				return usageError{err}
			}
			c := a.client()
			if a.dryRun {
				fmt.Fprintln(a.stdout, c.URL(oaipmh.Path, req.Values()))
				return nil
			}
			resp, raw, err := oaipmh.Do(cmd.Context(), c, req)
			if err != nil {
				return err
			}
			if a.format == "xml" {
				return a.writeRaw(raw)
			}
			recs, err := resp.Common()
			if err != nil {
				return err
			}
			return output.Records(a.stdout, recs, output.Meta{Source: "oaipmh"}, a.outputOptions())
		},
	}
}

func (a *app) oaiListCmd(use, short, verb string, o *oaiOpts) *cobra.Command {
	var limit int
	follow := true
	var out string
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  noArgsUsage,
		RunE: func(cmd *cobra.Command, args []string) error {
			o.limit, o.follow, o.out = limit, follow, out
			return a.runHarvest(cmd, verb, o)
		},
	}
	f := cmd.Flags()
	f.IntVar(&limit, "limit", 0, "stop after this many items (0 = no limit)")
	f.BoolVar(&follow, "follow", true, "follow resumptionTokens (--follow=false fetches one page)")
	if verb != oaipmh.VerbListSets {
		f.StringVar(&out, "out", "", "append records as JSON Lines to FILE (stdout then only gets progress)")
		cmd.Example = fmt.Sprintf(`  ndl oaipmh %s --set aozora --from 2026-01-01 --limit 500 --format jsonl
  ndl harvest %s --set iss-ndl-opac --from 2026-10-01 --until 2026-10-31 --out ndl.jsonl
  ndl harvest %s --token 'dcndl/2026-10-01T00:00:00Z/...' --out ndl.jsonl   # resume`, use, use, use)
	}
	return cmd
}

func (a *app) runHarvest(cmd *cobra.Command, verb string, o *oaiOpts) error {
	if o.limit < 0 {
		return usagef("--limit must be >= 0")
	}
	req := oaipmh.Request{Verb: verb, ResumptionToken: o.token}
	if o.token == "" && verb != oaipmh.VerbListSets {
		req.MetadataPrefix, req.From, req.Until, req.Set = o.prefix, o.from, o.until, o.set
	}
	if err := req.Validate(); err != nil {
		return usageError{err}
	}
	if o.out != "" && cmd.Flags().Changed("format") && a.format != "jsonl" {
		return usagef("--out always writes JSON Lines; drop --format or use --format jsonl")
	}
	c := a.client()
	if a.dryRun {
		fmt.Fprintln(a.stdout, c.URL(oaipmh.Path, req.Values()))
		return nil
	}

	sink := a.stdout
	format := a.format
	if o.out != "" {
		f, err := os.OpenFile(o.out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return usageError{err}
		}
		defer f.Close()
		sink, format = f, "jsonl"
	}

	var (
		acc   []dcndl.Record
		sets  []oaipmh.Set
		total int
		empty bool
	)
	res, err := oaipmh.Harvest(cmd.Context(), c, req, oaipmh.HarvestOptions{Limit: o.limit, Follow: o.follow}, func(p *oaipmh.Page) error {
		if p.Resp.NoRecords() {
			empty = true
		}
		total += p.Resp.Len()
		if p.CompleteListSize > 0 {
			a.info("[harvest] %d/%d items, token=%s", total, p.CompleteListSize, orNone(p.Token))
		} else {
			a.info("[harvest] %d items, token=%s", total, orNone(p.Token))
		}
		if format == "xml" {
			return a.writeRaw(p.Raw)
		}
		if verb == oaipmh.VerbListSets {
			if format == "jsonl" {
				for _, s := range p.Resp.Sets {
					if err := a.writeJSONLine(sink, s); err != nil {
						return err
					}
				}
				return nil
			}
			sets = append(sets, p.Resp.Sets...)
			return nil
		}
		recs, err := p.Resp.Common()
		if err != nil {
			return err
		}
		if format == "jsonl" {
			return output.JSONL(sink, recs, a.outFields)
		}
		acc = append(acc, recs...)
		return nil
	})

	// Render what was collected even after an interruption.
	if format == "table" || format == "json" {
		var werr error
		if verb == oaipmh.VerbListSets {
			werr = a.writeSets(sets)
		} else {
			meta := output.Meta{Source: "oaipmh", ResumptionToken: res.NextToken}
			werr = output.Records(a.stdout, acc, meta, a.outputOptions())
		}
		if err == nil {
			err = werr
		}
	}
	if empty {
		a.info("noRecordsMatch: no records in the requested range")
	}
	if o.out != "" {
		a.info("[harvest] wrote %d records to %s", res.Fetched, o.out)
	}

	if res.NextToken != "" {
		hint := fmt.Sprintf("ndl oaipmh %s --token '%s'", cmd.Name(), res.NextToken)
		if o.out != "" {
			hint += fmt.Sprintf(" --out '%s'", o.out)
		}
		switch {
		case errors.Is(err, context.Canceled):
			fmt.Fprintf(a.stderr, "\ninterrupted after %d items. resume with:\n  %s\n", res.Fetched, hint)
		case err != nil:
			fmt.Fprintf(a.stderr, "stopped after %d items. resume with:\n  %s\n", res.Fetched, hint)
		default:
			a.info("more items remain. continue with:\n  %s", hint)
		}
	} else if errors.Is(err, context.Canceled) {
		fmt.Fprintf(a.stderr, "\ninterrupted after %d items.\n", res.Fetched)
	}
	return err
}

func (a *app) writeSets(sets []oaipmh.Set) error {
	if a.format == "table" {
		rows := make([][]string, len(sets))
		for i, s := range sets {
			rows[i] = []string{s.Spec, s.Name}
		}
		return output.Grid(a.stdout, []string{"SET", "NAME"}, rows, map[int]int{1: 1}, 0)
	}
	return a.writeJSONValue(sets)
}

func (a *app) writeJSONValue(v any) error {
	enc := json.NewEncoder(a.stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func (a *app) writeJSONLine(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
