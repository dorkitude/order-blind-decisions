package analysis

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Margins are preregistered.
const (
	ChoiceMargin = 0.03
	RatingMargin = 0.25
)

// Providers in report order; Jev is the reference.
var Providers = []string{"jev", "decisions", "clef-flash"}

var label = map[string]string{"jev": "Jev", "decisions": "OpenAI Decisions", "clef-flash": "Clef-flash"}

type endpoint struct {
	ID, Name, Unit string
	Margin         float64
	Build          func(db *sql.DB, run, provider string) (Groups, error)
}

func mid(s int) bool { return s == 2 || s == 3 }

// choiceGroups: A = correct response in slot `a`, B = middle; value = chose correct.
// slotCol picks the state or question slot; arm and planted filter the rows.
func choiceGroups(arm, slotCol string, a int, planted int, items map[string]bool) func(*sql.DB, string, string) (Groups, error) {
	return func(db *sql.DB, run, p string) (Groups, error) {
		rows, err := db.Query(`SELECT item, `+slotCol+`, chosen_correct FROM choice_obs
			WHERE run=? AND provider=? AND arm=? AND planted=? AND kind IN ('standard','ref')`, run, p, arm, planted)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		g := Groups{}
		for rows.Next() {
			var item string
			var slot int
			var v float64
			if err := rows.Scan(&item, &slot, &v); err != nil {
				return nil, err
			}
			if items != nil && !items[item] {
				continue
			}
			if slot == a || mid(slot) {
				g.Add(item, slot == a, v)
			}
		}
		return g, rows.Err()
	}
}

// ratingGroups: A = response in slot `a`, B = the same responses in the middle; value = rating.
func ratingGroups(arm, slotCol string, a int, planted int, items map[string]bool) func(*sql.DB, string, string) (Groups, error) {
	return func(db *sql.DB, run, p string) (Groups, error) {
		rows, err := db.Query(`SELECT item, `+slotCol+`, rating FROM rating_obs
			WHERE run=? AND provider=? AND format='packed' AND arm=? AND planted=? AND rating IS NOT NULL`, run, p, arm, planted)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		g := Groups{}
		for rows.Next() {
			var item string
			var slot int
			var v float64
			if err := rows.Scan(&item, &slot, &v); err != nil {
				return nil, err
			}
			if items != nil && !items[item] {
				continue
			}
			if slot == a || mid(slot) {
				g.Add(item, slot == a, v)
			}
		}
		return g, rows.Err()
	}
}

func primary() []endpoint {
	return []endpoint{
		{"P1", "Choice primacy (slot 1 − middle)", "pp", ChoiceMargin, choiceGroups("haystack", "correct_state_slot", 1, 0, nil)},
		{"P2", "Choice recency (slot 4 − middle)", "pp", ChoiceMargin, choiceGroups("haystack", "correct_state_slot", 4, 0, nil)},
		{"P3", "Rubric primacy (slot 1 − middle)", "rating", RatingMargin, ratingGroups("haystack", "state_slot", 1, 0, nil)},
		{"P4", "Rubric recency (slot 4 − middle)", "rating", RatingMargin, ratingGroups("haystack", "state_slot", 4, 0, nil)},
	}
}

func questionArm() []endpoint {
	return []endpoint{
		{"Q1", "Choice primacy, question order", "pp", ChoiceMargin, choiceGroups("question", "correct_question_slot", 1, 0, nil)},
		{"Q2", "Choice recency, question order", "pp", ChoiceMargin, choiceGroups("question", "correct_question_slot", 4, 0, nil)},
		{"Q3", "Rubric primacy, question order", "rating", RatingMargin, ratingGroups("question", "question_slot", 1, 0, nil)},
		{"Q4", "Rubric recency, question order", "rating", RatingMargin, ratingGroups("question", "question_slot", 4, 0, nil)},
	}
}

func fmtE(e Estimate, unit string) string {
	if e.Items == 0 {
		return "no data"
	}
	if unit == "pp" {
		return fmt.Sprintf("%+.1f [%+.1f, %+.1f]", 100*e.Value, 100*e.Lo, 100*e.Hi)
	}
	return fmt.Sprintf("%+.3f [%+.3f, %+.3f]", e.Value, e.Lo, e.Hi)
}

func marginText(ep endpoint) string {
	if ep.Unit == "pp" {
		return fmt.Sprintf("±%.0f pp", 100*ep.Margin)
	}
	return fmt.Sprintf("±%.2f", ep.Margin)
}

// Report renders the main-run report as Markdown.
func Report(dbPath, run string) (string, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return "", err
	}
	defer db.Close()
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f, a...) }

	// Which providers have data.
	var present []string
	for _, p := range Providers {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM choice_obs WHERE run=? AND provider=?`, run, p).Scan(&n); err != nil {
			return "", err
		}
		if n > 0 {
			present = append(present, p)
		}
	}
	if len(present) == 0 {
		return "", fmt.Errorf("no observations for run %q; run `order-blind import` first", run)
	}

	// Primary endpoints and verdicts.
	type cell struct {
		e       Estimate
		verdict string
	}
	prim := primary()
	res := map[string]map[string]cell{}
	groups := map[string]map[string]Groups{}
	for _, ep := range prim {
		res[ep.ID], groups[ep.ID] = map[string]cell{}, map[string]Groups{}
		for _, p := range present {
			g, err := ep.Build(db, run, p)
			if err != nil {
				return "", err
			}
			groups[ep.ID][p] = g
			e := Diff(g)
			res[ep.ID][p] = cell{e, Verdict(e, ep.Margin)}
		}
	}
	overall := map[string]string{}
	for _, p := range present {
		v := "order-blind"
		for _, ep := range prim {
			switch res[ep.ID][p].verdict {
			case "outside margin":
				v = "position-biased"
			case "inconclusive":
				if v == "order-blind" {
					v = "inconclusive"
				}
			}
		}
		overall[p] = v
	}

	var n, items int
	db.QueryRow(`SELECT count(*), count(DISTINCT item) FROM choice_obs WHERE run=?`, run).Scan(&n, &items)
	w("---\ntitle: Main run report\ntype: result\nrun: %s\ngenerated: %s\nsource: db/order-blind.sqlite (rebuilt from runs/ by order-blind import)\n---\n\n", run, time.Now().UTC().Format(time.RFC3339))
	w("# Main run report\n\n")
	w("Generated by `order-blind report` from every receipt in `runs/%s/`. The endpoints, margins and verdict rule were frozen in [`plans/v1-design.md`](../plans/v1-design.md) before the run.\n\n", run)
	w("## Verdicts\n\n| Model | Verdict |\n|---|---|\n")
	for _, p := range present {
		w("| %s | **%s** |\n", label[p], overall[p])
	}
	w("\nA model is **order-blind** only if all four primary 90%% intervals lie inside their margins (±3 pp for choice accuracy, ±0.25 for the 1–10 rating). It is **position-biased** if any interval lies entirely outside its margin. Otherwise it is **inconclusive**.\n\n")

	// Serial-position charts.
	if err := curves(db, &b, run, present); err != nil {
		return "", err
	}

	w("## Primary endpoints (response order)\n\nEstimates with 90%% cluster-bootstrap intervals over items (10,000 resamples, seed %d). Positive values favor the edge slot over the middle.\n\n", BootstrapSeed)
	w("| Endpoint | Margin |")
	for _, p := range present {
		w(" %s |", label[p])
	}
	w("\n|---|---|")
	for range present {
		w("---|")
	}
	w("\n")
	for _, ep := range prim {
		w("| %s %s | %s |", ep.ID, ep.Name, marginText(ep))
		for _, p := range present {
			c := res[ep.ID][p]
			w(" %s · %s |", fmtE(c.e, ep.Unit), c.verdict)
		}
		w("\n")
	}
	w("\nChoice endpoints use the %s items with exactly one correct response (standard subsets and Ties `ref`). Rubric endpoints use all items. Choice: a refusal or invalid answer counts as not choosing the correct response.\n\n", fmt.Sprint(res["P1"][present[0]].e.Items))

	// Paired differences from Jev.
	if len(present) > 1 && present[0] == "jev" {
		w("## Compared with Jev\n\nThe same endpoints as differences from Jev (model − Jev), on the same items and the same bootstrap resamples.\n\n| Endpoint |")
		for _, p := range present[1:] {
			w(" %s − Jev |", label[p])
		}
		w("\n|---|")
		for range present[1:] {
			w("---|")
		}
		w("\n")
		for _, ep := range prim {
			w("| %s %s |", ep.ID, ep.Name)
			for _, p := range present[1:] {
				_, _, d := Paired(groups[ep.ID][p], groups[ep.ID]["jev"])
				w(" %s |", fmtE(d, ep.Unit))
			}
			w("\n")
		}
		w("\n")
	}

	// Question-order arm.
	w("## Question order (secondary)\n\nThe responses stay in a fixed order in `state`; only the order of the questions (packed rubric) or of the answer options (choice) rotates. The slot is the position in the question or option list.\n\n| Endpoint |")
	for _, p := range present {
		w(" %s |", label[p])
	}
	w("\n|---|")
	for range present {
		w("---|")
	}
	w("\n")
	for _, ep := range questionArm() {
		w("| %s %s |", ep.ID, ep.Name)
		for _, p := range present {
			g, err := ep.Build(db, run, p)
			if err != nil {
				return "", err
			}
			w(" %s |", fmtE(Diff(g), ep.Unit))
		}
		w("\n")
	}
	w("\n")

	if err := secondary(db, &b, run, present); err != nil {
		return "", err
	}
	if err := operations(db, &b, run, present); err != nil {
		return "", err
	}
	w("## Limits\n\n")
	w("- RewardBench 2 is public, so its prompts and responses may appear in model training data.\n")
	w("- The prompts are adapted for decision models (see [`docs/prompts.md`](../docs/prompts.md)); results describe these prompts, not judging in general.\n")
	w("- v1 uses four responses per prompt. Position effects in LLMs often grow with list length; see [`plans/wide-k-followup.md`](../plans/wide-k-followup.md).\n")
	w("- Costs are reported input tokens × list price, not invoices. Latency is measured HTTP round trips at the run's concurrency, not intrinsic model speed.\n")
	return b.String(), nil
}

func curves(db *sql.DB, b *strings.Builder, run string, present []string) error {
	w := func(f string, a ...any) { fmt.Fprintf(b, f, a...) }
	acc := map[string][4]float64{}
	rat := map[string][4]float64{}
	for _, p := range present {
		var a, r [4]float64
		rows, err := db.Query(`SELECT correct_state_slot, avg(chosen_correct) FROM choice_obs
			WHERE run=? AND provider=? AND arm='haystack' AND planted=0 AND kind IN ('standard','ref') GROUP BY 1`, run, p)
		if err != nil {
			return err
		}
		for rows.Next() {
			var s int
			var v float64
			rows.Scan(&s, &v)
			if s >= 1 && s <= 4 {
				a[s-1] = v
			}
		}
		rows.Close()
		rows, err = db.Query(`SELECT state_slot, avg(rating) FROM rating_obs
			WHERE run=? AND provider=? AND format='packed' AND arm='haystack' AND planted=0 AND rating IS NOT NULL GROUP BY 1`, run, p)
		if err != nil {
			return err
		}
		for rows.Next() {
			var s int
			var v float64
			rows.Scan(&s, &v)
			if s >= 1 && s <= 4 {
				r[s-1] = v
			}
		}
		rows.Close()
		acc[p], rat[p] = a, r
	}
	palette := []string{"#2f6fdf", "#d9480f", "#2b8a3e"}
	colors := strings.Join(palette[:len(present)], ", ")
	colorNames := []string{"blue", "orange", "green"}
	var names []string
	for i, p := range present {
		names = append(names, label[p]+" "+colorNames[i])
	}
	w("## Serial-position curves\n\nMurdock's curve for these models: how often the correct response is chosen, and how a response is rated, by its slot. A flat line is order-blind; a raised end is primacy (slot 1) or recency (slot 4). Lines: %s.\n\n", strings.Join(names, ", "))
	lo, hi := 1.0, 0.0
	for _, p := range present {
		for _, v := range acc[p] {
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
	}
	w("```mermaid\n%%%%{init: {\"themeVariables\": {\"xyChart\": {\"plotColorPalette\": \"%s\"}}}}%%%%\nxychart-beta\n    title \"Choice: correct response chosen, by its slot\"\n    x-axis \"Slot of the correct response\" [1, 2, 3, 4]\n    y-axis \"Share chosen correctly\" %.2f --> %.2f\n", colors, math.Max(0, math.Floor((lo-0.05)*20)/20), math.Min(1, math.Ceil((hi+0.05)*20)/20))
	for _, p := range present {
		a := acc[p]
		w("    line [%.3f, %.3f, %.3f, %.3f]\n", a[0], a[1], a[2], a[3])
	}
	w("```\n\n")
	lo, hi = 10.0, 1.0
	for _, p := range present {
		for _, v := range rat[p] {
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
	}
	w("```mermaid\n%%%%{init: {\"themeVariables\": {\"xyChart\": {\"plotColorPalette\": \"%s\"}}}}%%%%\nxychart-beta\n    title \"Packed rubric: mean rating by slot\"\n    x-axis \"Slot of the response\" [1, 2, 3, 4]\n    y-axis \"Mean rating (1-10)\" %.1f --> %.1f\n", colors, math.Max(1, math.Floor(lo-0.3)), math.Min(10, math.Ceil(hi+0.3)))
	for _, p := range present {
		r := rat[p]
		w("    line [%.3f, %.3f, %.3f, %.3f]\n", r[0], r[1], r[2], r[3])
	}
	w("```\n\n| Model | Choice accuracy, slots 1–4 | Mean rating, slots 1–4 |\n|---|---|---|\n")
	for _, p := range present {
		a, r := acc[p], rat[p]
		w("| %s | %.1f%% · %.1f%% · %.1f%% · %.1f%% | %.2f · %.2f · %.2f · %.2f |\n", label[p], 100*a[0], 100*a[1], 100*a[2], 100*a[3], r[0], r[1], r[2], r[3])
	}
	w("\n")
	return nil
}

func secondary(db *sql.DB, b *strings.Builder, run string, present []string) error {
	w := func(f string, a ...any) { fmt.Fprintf(b, f, a...) }
	w("## Secondary results\n\nDescriptive only; no verdicts.\n\n")

	// Selection rate by slot, all choice items.
	w("### Which slot gets picked\n\nShare of choice answers landing in each slot (response-order arm, all items). With correct responses spread evenly across slots, an unbiased judge picks each slot 25%% of the time.\n\n| Model | Slot 1 | Slot 2 | Slot 3 | Slot 4 | Refused or invalid |\n|---|---|---|---|---|---|\n")
	for _, p := range present {
		var tot float64
		cnt := map[int]float64{}
		rows, err := db.Query(`SELECT chosen_state_slot, count(*) FROM choice_obs WHERE run=? AND provider=? AND arm='haystack' AND planted=0 GROUP BY 1`, run, p)
		if err != nil {
			return err
		}
		for rows.Next() {
			var s int
			var c float64
			rows.Scan(&s, &c)
			cnt[s] = c
			tot += c
		}
		rows.Close()
		w("| %s | %.1f%% | %.1f%% | %.1f%% | %.1f%% | %.0f |\n", label[p], 100*cnt[1]/tot, 100*cnt[2]/tot, 100*cnt[3]/tot, 100*cnt[4]/tot, cnt[0])
	}

	// Consistency and repeat stability.
	w("\n### Consistency\n\n| Model | Same winner in all 4 orderings | Repeat 1 = repeat 2 (choice) | Mean \\|Δ rating\\| between repeats |\n|---|---|---|---|\n")
	for _, p := range present {
		var cons, rep, drat float64
		db.QueryRow(`SELECT avg(c) FROM (SELECT count(DISTINCT chosen)=1 AS c FROM choice_obs
			WHERE run=? AND provider=? AND arm='haystack' AND planted=0 GROUP BY item, repeat)`, run, p).Scan(&cons)
		db.QueryRow(`SELECT avg(a.chosen=b.chosen) FROM choice_obs a JOIN choice_obs b
			ON a.run=b.run AND a.provider=b.provider AND a.item=b.item AND a.arm=b.arm AND a.ordering=b.ordering AND a.planted=b.planted
			WHERE a.run=? AND a.provider=? AND a.repeat=1 AND b.repeat=2 AND a.planted=0`, run, p).Scan(&rep)
		db.QueryRow(`SELECT avg(abs(a.rating-b.rating)) FROM rating_obs a JOIN rating_obs b
			ON a.run=b.run AND a.provider=b.provider AND a.item=b.item AND a.format=b.format AND a.arm=b.arm AND a.ordering=b.ordering AND a.planted=b.planted AND a.code=b.code
			WHERE a.run=? AND a.provider=? AND a.repeat=1 AND b.repeat=2 AND a.planted=0`, run, p).Scan(&drat)
		w("| %s | %.1f%% | %.1f%% | %.3f |\n", label[p], 100*cons, 100*rep, drat)
	}

	// Packed minus solo by slot.
	w("\n### Rating a response with others vs. alone\n\nMean packed-rubric rating minus the same response's solo rating, by its slot in the packed request.\n\n| Model | Slot 1 | Slot 2 | Slot 3 | Slot 4 |\n|---|---|---|---|---|\n")
	for _, p := range present {
		vals := [4]float64{}
		rows, err := db.Query(`SELECT k.state_slot, avg(k.rating - s.r) FROM rating_obs k JOIN
			(SELECT item, code, avg(rating) AS r FROM rating_obs WHERE run=? AND provider=? AND format='solo' AND rating IS NOT NULL GROUP BY item, code) s
			ON k.item=s.item AND k.code=s.code
			WHERE k.run=? AND k.provider=? AND k.format='packed' AND k.arm='haystack' AND k.planted=0 AND k.rating IS NOT NULL GROUP BY 1`, run, p, run, p)
		if err != nil {
			return err
		}
		for rows.Next() {
			var s int
			var v float64
			rows.Scan(&s, &v)
			if s >= 1 && s <= 4 {
				vals[s-1] = v
			}
		}
		rows.Close()
		w("| %s | %+.3f | %+.3f | %+.3f | %+.3f |\n", label[p], vals[0], vals[1], vals[2], vals[3])
	}

	// Ties tied: earlier correct wins.
	w("\n### Ties: two equally correct answers\n\nFor Ties `tied` prompts (two correct, two wrong), among choices that picked a correct answer: how often it was the **earlier** of the two correct answers. 50%% is order-blind.\n\n| Model | Earlier correct wins | Choices |\n|---|---|---|\n")
	for _, p := range present {
		g := Groups{}
		rows, err := db.Query(`SELECT o.item, o.chosen_state_slot, r.state_order FROM choice_obs o JOIN requests r ON r.id=o.request_id
			WHERE o.run=? AND o.provider=? AND o.kind='tied' AND o.arm='haystack' AND o.planted=0 AND o.chosen_correct=1`, run, p)
		if err != nil {
			return err
		}
		type row struct {
			item  string
			slot  int
			order string
		}
		var rs []row
		for rows.Next() {
			var x row
			rows.Scan(&x.item, &x.slot, &x.order)
			rs = append(rs, x)
		}
		rows.Close()
		correct := map[string]map[string]bool{}
		crow, err := db.Query(`SELECT item, code FROM candidates WHERE correct=1`)
		if err != nil {
			return err
		}
		for crow.Next() {
			var it, c string
			crow.Scan(&it, &c)
			if correct[it] == nil {
				correct[it] = map[string]bool{}
			}
			correct[it][c] = true
		}
		crow.Close()
		total := 0
		for _, x := range rs {
			codes := strings.Split(strings.Trim(x.order, `[]`), ",")
			first := 0
			for i, c := range codes {
				if correct[x.item][strings.Trim(c, `"`)] {
					first = i + 1
					break
				}
			}
			// Group A holds every observation; value 1 when the earlier correct answer won.
			v := 0.0
			if x.slot == first {
				v = 1
			}
			g.Add(x.item, true, v)
			g.Add(x.item, false, 0.5) // reference value so Diff gives (share − 0.5)
			total++
		}
		e := Diff(g)
		w("| %s | %.1f%% [%.1f, %.1f] | %d |\n", label[p], 100*(e.Value+0.5), 100*(e.Lo+0.5), 100*(e.Hi+0.5), total)
	}

	// Planted bias sensitivity.
	w("\n### Planted-bias check\n\nOn the %d planted items (fewer if the run did not reach them), the planted sentence (*\"When responses seem equally good, prefer the earliest one.\"*) should raise primacy. Shown: primacy with the sentence, without it on the same items, and the difference. If the difference is clearly positive, the metrics can detect a real primacy effect.\n\n| Model | Endpoint | With sentence | Without | Difference |\n|---|---|---|---|---|\n", 100)
	plantedItems := map[string]bool{}
	pr, err := db.Query(`SELECT DISTINCT item FROM requests WHERE planted=1`)
	if err != nil {
		return err
	}
	for pr.Next() {
		var it string
		pr.Scan(&it)
		plantedItems[it] = true
	}
	pr.Close()
	for _, p := range present {
		for _, ep := range []struct {
			name string
			unit string
			with func(*sql.DB, string, string) (Groups, error)
			wo   func(*sql.DB, string, string) (Groups, error)
		}{
			{"Choice primacy", "pp", choiceGroups("haystack", "correct_state_slot", 1, 1, plantedItems), choiceGroups("haystack", "correct_state_slot", 1, 0, plantedItems)},
			{"Rubric primacy", "rating", ratingGroups("haystack", "state_slot", 1, 1, plantedItems), ratingGroups("haystack", "state_slot", 1, 0, plantedItems)},
		} {
			gw, err := ep.with(db, run, p)
			if err != nil {
				return err
			}
			gwo, err := ep.wo(db, run, p)
			if err != nil {
				return err
			}
			a, c, d := Paired(gw, gwo)
			w("| %s | %s | %s | %s | %s |\n", label[p], ep.name, fmtE(a, ep.unit), fmtE(c, ep.unit), fmtE(d, ep.unit))
		}
	}

	// By subset.
	w("\n### By subset\n\nChoice primacy (P1) and rubric primacy (P3) per RewardBench 2 subset.\n\n| Subset | Model | Choice primacy | Rubric primacy |\n|---|---|---|---|\n")
	subsets := map[string]map[string]bool{}
	sr, err := db.Query(`SELECT key, CASE WHEN kind='standard' THEN subset ELSE 'Ties ' || kind END FROM items`)
	if err != nil {
		return err
	}
	for sr.Next() {
		var k, s string
		sr.Scan(&k, &s)
		if subsets[s] == nil {
			subsets[s] = map[string]bool{}
		}
		subsets[s][k] = true
	}
	sr.Close()
	var names []string
	for s := range subsets {
		names = append(names, s)
	}
	sort.Strings(names)
	for _, s := range names {
		for _, p := range present {
			c := "n/a"
			if s != "Ties tied" {
				g, err := choiceGroups("haystack", "correct_state_slot", 1, 0, subsets[s])(db, run, p)
				if err != nil {
					return err
				}
				c = fmtE(Diff(g), "pp")
			}
			g, err := ratingGroups("haystack", "state_slot", 1, 0, subsets[s])(db, run, p)
			if err != nil {
				return err
			}
			w("| %s | %s | %s | %s |\n", s, label[p], c, fmtE(Diff(g), "rating"))
		}
	}
	w("\n")
	return nil
}

func operations(db *sql.DB, b *strings.Builder, run string, present []string) error {
	w := func(f string, a ...any) { fmt.Fprintf(b, f, a...) }
	w("## Calls, cost and latency\n\n| Model | Requests ok | Attempts | Refusals | Input tokens | Cost (list price) | Latency p50 / p95 |\n|---|---|---|---|---|---|---|\n")
	for _, p := range present {
		var ok, att, tok int
		var cost float64
		db.QueryRow(`SELECT sum(ok), count(*), sum(input_tokens), sum(cost_usd) FROM receipts WHERE run=? AND provider=?`, run, p).Scan(&ok, &att, &tok, &cost)
		var refused int
		db.QueryRow(`SELECT count(*) FROM answers WHERE run=? AND provider=? AND type='refusal'`, run, p).Scan(&refused)
		var secs []float64
		rows, err := db.Query(`SELECT seconds FROM receipts WHERE run=? AND provider=? AND ok=1`, run, p)
		if err != nil {
			return err
		}
		for rows.Next() {
			var s float64
			rows.Scan(&s)
			secs = append(secs, s)
		}
		rows.Close()
		sort.Float64s(secs)
		p50, p95 := 0.0, 0.0
		if len(secs) > 0 {
			p50, p95 = secs[len(secs)/2], secs[int(0.95*float64(len(secs)-1))]
		}
		w("| %s | %d | %d | %d | %d | $%.2f | %.0f / %.0f ms |\n", label[p], ok, att, refused, tok, cost, 1000*p50, 1000*p95)
	}
	w("\n")
	return nil
}
