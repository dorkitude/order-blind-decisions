// Command order-blind runs the order-blind-decisions study: it fetches
// RewardBench 2, freezes the request plan, probes provider limits, and sends
// planned requests to Jev, OpenAI Decisions and Cloudflare Clef-flash.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/dorkitude/order-blind-decisions/internal/analysis"
	"github.com/dorkitude/order-blind-decisions/internal/answer"
	"github.com/dorkitude/order-blind-decisions/internal/dataset"
	"github.com/dorkitude/order-blind-decisions/internal/design"
	"github.com/dorkitude/order-blind-decisions/internal/frozen"
	"github.com/dorkitude/order-blind-decisions/internal/probe"
	"github.com/dorkitude/order-blind-decisions/internal/provider"
	"github.com/dorkitude/order-blind-decisions/internal/runlog"
	"github.com/dorkitude/order-blind-decisions/internal/runner"
	"github.com/dorkitude/order-blind-decisions/internal/store"
)

const (
	dataDir   = "data"
	frozenDir = "frozen/v1"
	runsDir   = "runs"
	dbPath    = "db/order-blind.sqlite"
)

func main() {
	root := &cobra.Command{
		Use:           "order-blind",
		Short:         "Primacy and recency tests for decision models",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(fetchCmd(), planCmd(), probeCmd(), runCmd(), statusCmd(), importCmd(), reportCmd())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func fetchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fetch",
		Short: "Download the pinned RewardBench 2 test split and verify its hash",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := dataset.Fetch(dataDir)
			if err != nil {
				return err
			}
			fmt.Printf("verified %s (revision %s)\n", p, dataset.Revision)
			return nil
		},
	}
}

func planCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "plan",
		Short: "Build items and every request body, and freeze them in frozen/v1",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rows, err := dataset.Load(dataset.Path(dataDir))
			if err != nil {
				return fmt.Errorf("%w (run `order-blind fetch` first)", err)
			}
			items, err := design.BuildItems(rows)
			if err != nil {
				return err
			}
			planted := design.Planted(items)
			var reqs []design.Request
			bytesBy := map[string]int{}
			countBy := map[string]int{}
			for _, it := range items {
				rs, err := it.Plan(planted[it.Key])
				if err != nil {
					return err
				}
				for _, r := range rs {
					k := r.Format + "/" + r.Arm
					if r.Planted {
						k = r.Format + "/planted"
					}
					countBy[k]++
					bytesBy[k] += r.BodyBytes
				}
				reqs = append(reqs, rs...)
			}
			if err := frozen.Write(frozenDir, items, reqs); err != nil {
				return err
			}
			total := 0
			keys := make([]string, 0, len(countBy))
			for k := range countBy {
				keys = append(keys, k)
				total += bytesBy[k]
			}
			sort.Strings(keys)
			fmt.Printf("%d items, %d requests per provider written to %s\n", len(items), len(reqs), frozenDir)
			for _, k := range keys {
				fmt.Printf("  %-18s %6d requests, %6.1f MB\n", k, countBy[k], float64(bytesBy[k])/1e6)
			}
			estimate(float64(total))
			return nil
		},
	}
}

// estimate prints list-price accounting at 4 bytes per token (rough).
func estimate(bytes float64) {
	tok := bytes / 4
	fmt.Printf("estimated input tokens per provider: %.1fM (at 4 bytes/token; rough)\n", tok/1e6)
	sum := 0.0
	for _, name := range provider.Names {
		price := map[string]float64{"jev": 0.042, "decisions": 0.10, "clef-flash": 0.09, "msd1": 0.042}[name]
		fmt.Printf("  %-11s ~$%.2f\n", name, tok*price/1e6)
		sum += tok * price / 1e6
	}
	fmt.Printf("  %-11s ~$%.2f\n", "all", sum)
}

func providers(names []string) ([]provider.Provider, error) {
	var ps []provider.Provider
	for _, n := range names {
		p, err := provider.New(n)
		if err != nil {
			return nil, err
		}
		ps = append(ps, p)
	}
	return ps, nil
}

type runFlags struct {
	providers []string
	workers   int
	workersBy map[string]int
	budget    float64
	live      bool
}

func (f *runFlags) bind(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&f.providers, "providers", provider.Names, "providers to call")
	cmd.Flags().IntVar(&f.workers, "workers", 4, "concurrent requests per provider")
	cmd.Flags().StringToIntVar(&f.workersBy, "provider-workers", nil, "per-provider workers, e.g. clef-flash=16,jev=8")
	cmd.Flags().Float64Var(&f.budget, "budget", 100, "project-wide spending cap in USD, across all runs")
	cmd.Flags().BoolVar(&f.live, "live", false, "actually call the providers (paid)")
}

func execute(ctx context.Context, f runFlags, run string, jobs []runner.Job) error {
	if !f.live {
		fmt.Printf("dry run: %d requests × %d providers in run %q; add --live to send\n", len(jobs), len(f.providers), run)
		return nil
	}
	ps, err := providers(f.providers)
	if err != nil {
		return err
	}
	sums, err := runner.Run(ctx, runner.Options{
		Root: runsDir, Run: run, Workers: f.workers, WorkersBy: f.workersBy, BudgetUSD: f.budget,
		MaxAttempts: 4, MaxConsecFail: 20, Log: os.Stdout, ProgressEvery: 500,
	}, ps, jobs)
	for _, s := range sums {
		fmt.Printf("%-11s planned %d, skipped %d (done earlier), ok %d, failed %d, attempts %d, cost $%.4f%s\n",
			s.Provider, s.Planned, s.Skipped, s.OK, s.Failed, s.Attempts, s.CostUSD, stopped(s.StoppedBy))
	}
	return err
}

func stopped(why string) string {
	if why == "" {
		return ""
	}
	return ", stopped: " + why
}

func probeCmd() *cobra.Command {
	var f runFlags
	cmd := &cobra.Command{
		Use:   "probe",
		Short: "Find how much state each provider reads (needle at start or end of growing text)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			jobs, err := probe.Jobs(design.CanonicalModel)
			if err != nil {
				return err
			}
			if err := execute(cmd.Context(), f, "probe", jobs); err != nil {
				return err
			}
			if f.live {
				return probeSummary()
			}
			return nil
		},
	}
	f.bind(cmd)
	return cmd
}

func probeSummary() error {
	type cell struct{ ok, n, tokens int }
	cells := map[string]map[string]*cell{}
	needle := map[string]probe.Case{}
	for _, c := range probe.Cases() {
		needle[c.ID] = c
	}
	err := runlog.Walk(runsDir+"/probe", func(_ string, r runlog.Receipt) error {
		if !r.OK {
			return nil
		}
		c := needle[r.RequestID]
		body := r.Normalized
		if body == nil {
			body = r.Raw
		}
		a, err := answer.Parse(body)
		if err != nil {
			return err
		}
		k := fmt.Sprintf("%05d|%s", c.Length, c.Position)
		if cells[r.Provider] == nil {
			cells[r.Provider] = map[string]*cell{}
		}
		x := cells[r.Provider][k]
		if x == nil {
			x = &cell{}
			cells[r.Provider][k] = x
		}
		x.n++
		x.tokens = max(x.tokens, r.InputTokens)
		if a["evaluation"].Choice == c.Needle {
			x.ok++
		}
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Println("\nneedle found (of 2) by target length and position; reported input tokens in brackets")
	for _, p := range provider.Names {
		if cells[p] == nil {
			continue
		}
		fmt.Printf("%s\n", p)
		for _, n := range probe.Lengths {
			var parts []string
			for _, pos := range probe.Positions {
				x := cells[p][fmt.Sprintf("%05d|%s", n, pos)]
				if x == nil {
					parts = append(parts, pos+" -")
					continue
				}
				parts = append(parts, fmt.Sprintf("%s %d/%d [%d tok]", pos, x.ok, x.n, x.tokens))
			}
			fmt.Printf("  ~%5d tokens: %s\n", n, strings.Join(parts, "   "))
		}
	}
	return nil
}

func runCmd() *cobra.Command {
	var f runFlags
	var name string
	var pilotPerKind int
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Send the frozen plan (or a pilot subset of it) to the providers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			plan, err := frozen.Load(frozenDir)
			if err != nil {
				return err
			}
			var keep func(design.Request) bool
			if pilotPerKind > 0 {
				items := make([]design.Item, 0, len(plan.Order))
				for _, k := range plan.Order {
					items = append(items, plan.Items[k])
				}
				pilot := design.Pilot(items, pilotPerKind)
				keep = func(r design.Request) bool { return pilot[r.Item] }
			}
			jobs, err := plan.Jobs(keep)
			if err != nil {
				return err
			}
			return execute(cmd.Context(), f, name, jobs)
		},
	}
	f.bind(cmd)
	cmd.Flags().StringVar(&name, "run", "main", "run name; receipts go to runs/<run>/<provider>/")
	cmd.Flags().IntVar(&pilotPerKind, "pilot-per-kind", 0, "if set, only this many seeded items per subset and Ties kind")
	return cmd
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show spend and successful requests per run and provider",
		RunE: func(cmd *cobra.Command, _ []string) error {
			type agg struct {
				ok, attempts int
				cost         float64
			}
			by := map[string]*agg{}
			total := 0.0
			err := runlog.Walk(runsDir, func(_ string, r runlog.Receipt) error {
				k := r.Run + " / " + r.Provider
				if by[k] == nil {
					by[k] = &agg{}
				}
				by[k].attempts++
				by[k].cost += r.CostUSD
				if r.OK {
					by[k].ok++
				}
				total += r.CostUSD
				return nil
			})
			if err != nil {
				return err
			}
			keys := make([]string, 0, len(by))
			for k := range by {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Printf("%-26s %7d ok of %7d attempts  $%.4f\n", k, by[k].ok, by[k].attempts, by[k].cost)
			}
			fmt.Printf("project total $%.4f\n", total)
			return nil
		},
	}
}

func importCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import",
		Short: "Rebuild db/order-blind.sqlite from frozen/v1 and every receipt in runs/",
		RunE: func(cmd *cobra.Command, _ []string) error {
			counts, err := store.Import(dbPath, frozenDir, runsDir)
			if err != nil {
				return err
			}
			for _, t := range []string{"items", "requests", "receipts", "answers", "choice_obs", "rating_obs"} {
				fmt.Printf("%-11s %8d rows\n", t, counts[t])
			}
			fmt.Printf("wrote %s\n", dbPath)
			return nil
		},
	}
}

func reportCmd() *cobra.Command {
	var run, out string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Compute the preregistered endpoints and write a Markdown report",
		RunE: func(cmd *cobra.Command, _ []string) error {
			md, err := analysis.Report(dbPath, run)
			if err != nil {
				return err
			}
			if out == "-" {
				fmt.Print(md)
				return nil
			}
			if err := os.WriteFile(out, []byte(md), 0o644); err != nil {
				return err
			}
			fmt.Printf("wrote %s\n", out)
			return nil
		},
	}
	cmd.Flags().StringVar(&run, "run", "main", "run to report")
	cmd.Flags().StringVar(&out, "out", "results/main-report.md", "output path, or - for stdout")
	return cmd
}
