// Copyright 2026 ish-cs. MIT License. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newSectionsListCmd(flags *rootFlags) *cobra.Command {
	var flagSearch string
	var flagPage string

	cmd := &cobra.Command{
		Use:         "list",
		Short:       "Fetch the raw HTML search results for keyword + facet filters",
		Example:     "  berkeley-classes sections list",
		Annotations: map[string]string{"pp:endpoint": "sections.list", "pp:method": "GET", "pp:path": "/search/class", "mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := flags.newClient()
			if err != nil {
				return err
			}

			path := "/search/class"
			htmlRequestParams := map[string]string{}
			if flagSearch != "" {
				htmlRequestParams["search"] = fmt.Sprintf("%v", flagSearch)
			}
			if flagPage != "" {
				htmlRequestParams["page"] = fmt.Sprintf("%v", flagPage)
			}
			params := map[string]string{}
			if flagSearch != "" {
				params["search"] = fmt.Sprintf("%v", flagSearch)
			}
			if flagPage != "" {
				params["page"] = fmt.Sprintf("%v", flagPage)
			}
			data, prov, err := resolveReadWithStrategy(cmd.Context(), c, flags, "auto", "sections", true, path, params, nil, cmd.ErrOrStderr())
			if err != nil {
				return classifyAPIError(err, flags)
			}
			if !flags.dryRun {
				data, err = extractHTMLResponse(data, htmlExtractionOptions{
					Mode:           "page",
					BaseURL:        htmlExtractionRequestURL(c.BaseURL, path, htmlRequestParams),
					LinkPrefixes:   []string{},
					Limit:          0,
					ScriptSelector: "script#__NEXT_DATA__",
					JSONPath:       "",
				})
				if err != nil {
					return err
				}
			}
			// Print provenance to stderr for human-facing output only.
			// Machine-format flags (--json, --csv, --compact, --quiet, --plain,
			// --select) and piped stdout suppress this line; the JSON envelope
			// already carries meta.source for those consumers.
			// SYNC: keep this gate aligned with command_promoted.go.tmpl.
			if wantsHumanTable(cmd.OutOrStdout(), flags) {
				var countItems []json.RawMessage
				_ = json.Unmarshal(data, &countItems)
				printProvenance(cmd, len(countItems), prov)
			}
			// For JSON output, wrap with provenance envelope before passing through flags.
			// --select wins over --compact when both are set; --compact only runs when
			// no explicit fields were requested. Explicit format flags (--csv, --quiet,
			// --plain) opt out of the auto-JSON path so piped consumers that asked for
			// a non-JSON format reach the standard pipeline below.
			if flags.asJSON || (!isTerminal(cmd.OutOrStdout()) && !flags.csv && !flags.quiet && !flags.plain) {
				filtered := data
				if flags.selectFields != "" {
					filtered = filterFields(filtered, flags.selectFields)
				} else if flags.compact {
					filtered = compactFields(filtered)
				}
				wrapped, wrapErr := wrapWithProvenance(filtered, prov)
				if wrapErr != nil {
					return wrapErr
				}
				return printOutput(cmd.OutOrStdout(), wrapped, true)
			}
			// For all other output modes (table, csv, plain, quiet), use the standard pipeline
			if wantsHumanTable(cmd.OutOrStdout(), flags) {
				var items []map[string]any
				if json.Unmarshal(data, &items) == nil && len(items) > 0 {
					if err := printAutoTable(cmd.OutOrStdout(), items); err != nil {
						return err
					}
					if len(items) >= 25 {
						fmt.Fprintf(os.Stderr, "\nShowing %d results. To narrow: add --limit, --json --select, or filter flags.\n", len(items))
					}
					return nil
				}
			}
			return printOutputWithFlags(cmd.OutOrStdout(), data, flags)
		},
	}
	cmd.Flags().StringVar(&flagSearch, "keywords", "", "Search keywords (course code, title, instructor name)")
	cmd.Flags().StringVar(&flagSearch, "keyword", "", "Search keywords (course code, title, instructor name)")
	_ = cmd.Flags().MarkHidden("keyword")
	cmd.Flags().StringVar(&flagSearch, "q", "", "Search keywords (course code, title, instructor name)")
	_ = cmd.Flags().MarkHidden("q")
	cmd.Flags().StringVar(&flagPage, "page", "0", "Result page (zero-indexed)")

	return cmd
}
