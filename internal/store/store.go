// Package store rebuilds the analysis database db/order-blind.sqlite from
// the frozen plan and the receipts in runs/. The database is disposable:
// it is never committed and Import always starts from scratch.
//
// Tables:
//
//	items, candidates, requests   the frozen plan
//	receipts                      every HTTP attempt (bodies omitted)
//	answers                       typed answers from each request's successful receipt
//	choice_obs                    one row per choice answer, with slots resolved
//	rating_obs                    one row per rated response, with slots resolved
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/dorkitude/order-blind-decisions/internal/answer"
	"github.com/dorkitude/order-blind-decisions/internal/frozen"
	"github.com/dorkitude/order-blind-decisions/internal/runlog"
)

const schema = `
CREATE TABLE items (key TEXT PRIMARY KEY, rb2_id TEXT, subset TEXT, kind TEXT, prompt TEXT);
CREATE TABLE candidates (item TEXT, code TEXT, base_slot INTEGER, correct INTEGER, source TEXT, text TEXT,
  PRIMARY KEY (item, code));
CREATE TABLE requests (id TEXT PRIMARY KEY, item TEXT, kind TEXT, format TEXT, arm TEXT, ordering INTEGER,
  repeat INTEGER, planted INTEGER, state_order TEXT, question_order TEXT, body_sha256 TEXT, body_bytes INTEGER);
CREATE TABLE receipts (run TEXT, provider TEXT, request_id TEXT, attempt INTEGER, started_utc TEXT,
  seconds REAL, status INTEGER, ok INTEGER, response_model TEXT, input_tokens INTEGER, output_tokens INTEGER,
  usage_reported INTEGER, cost_usd REAL, cost_estimated INTEGER, error TEXT, parse_error TEXT, file TEXT);
CREATE TABLE answers (run TEXT, provider TEXT, request_id TEXT, question TEXT, type TEXT, choice TEXT,
  score REAL, probabilities TEXT, PRIMARY KEY (run, provider, request_id, question));
CREATE TABLE choice_obs (run TEXT, provider TEXT, request_id TEXT, item TEXT, subset TEXT, kind TEXT,
  arm TEXT, ordering INTEGER, repeat INTEGER, planted INTEGER,
  chosen TEXT, chosen_state_slot INTEGER, chosen_question_slot INTEGER, chosen_correct INTEGER,
  correct_state_slot INTEGER, correct_question_slot INTEGER, refused INTEGER,
  p_correct REAL);
CREATE TABLE rating_obs (run TEXT, provider TEXT, request_id TEXT, item TEXT, subset TEXT, kind TEXT,
  format TEXT, arm TEXT, ordering INTEGER, repeat INTEGER, planted INTEGER,
  code TEXT, correct INTEGER, state_slot INTEGER, question_slot INTEGER, rating REAL, modal_rating INTEGER,
  refused INTEGER);
CREATE INDEX choice_obs_run ON choice_obs (run, provider, arm, planted);
CREATE INDEX rating_obs_run ON rating_obs (run, provider, format, arm, planted);
`

func slotOf(order []string, code string) int {
	for i, c := range order {
		if c == code {
			return i + 1
		}
	}
	return 0
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Import rebuilds the database at dbPath. It returns counts by table.
func Import(dbPath, frozenDir, runsDir string) (map[string]int, error) {
	plan, err := frozen.Load(frozenDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	tmp := dbPath + ".building"
	os.Remove(tmp)
	db, err := sql.Open("sqlite", tmp)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	counts := map[string]int{}
	ins := func(table string, n int) (*sql.Stmt, error) {
		q := "INSERT INTO " + table + " VALUES (?"
		for i := 1; i < n; i++ {
			q += ",?"
		}
		return tx.Prepare(q + ")")
	}

	stItem, _ := ins("items", 5)
	stCand, _ := ins("candidates", 6)
	for _, k := range plan.Order {
		it := plan.Items[k]
		if _, err := stItem.Exec(it.Key, it.ID, it.Subset, it.Kind, it.Prompt); err != nil {
			return nil, err
		}
		for i, c := range it.Candidates {
			if _, err := stCand.Exec(it.Key, c.Code, i+1, b2i(c.Correct), c.Source, c.Text); err != nil {
				return nil, err
			}
		}
		counts["items"]++
	}
	stReq, _ := ins("requests", 12)
	for _, r := range plan.Requests {
		so, _ := json.Marshal(r.StateOrder)
		qo, _ := json.Marshal(r.QuestionOrder)
		if _, err := stReq.Exec(r.ID, r.Item, r.Kind, r.Format, r.Arm, r.Ordering, r.Repeat, b2i(r.Planted), string(so), string(qo), r.BodySHA256, r.BodyBytes); err != nil {
			return nil, err
		}
		counts["requests"]++
	}
	reqByID := map[string]int{}
	for i, r := range plan.Requests {
		reqByID[r.ID] = i
	}

	stRc, _ := ins("receipts", 17)
	type key struct{ run, provider, id string }
	final := map[key]runlog.Receipt{}
	err = runlog.Walk(runsDir, func(path string, r runlog.Receipt) error {
		rel, _ := filepath.Rel(runsDir, path)
		if _, err := stRc.Exec(r.Run, r.Provider, r.RequestID, r.Attempt, r.StartedUTC, r.Seconds, r.Status, b2i(r.OK),
			r.ResponseModel, r.InputTokens, r.OutputTokens, b2i(r.UsageReported), r.CostUSD, b2i(r.CostEstimated), r.Error, r.ParseError, rel); err != nil {
			return err
		}
		counts["receipts"]++
		if r.OK {
			final[key{r.Run, r.Provider, r.RequestID}] = r
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	stAns, _ := ins("answers", 8)
	stCh, _ := ins("choice_obs", 18)
	stRt, _ := ins("rating_obs", 18)
	for k, r := range final {
		i, planned := reqByID[k.id]
		if !planned {
			continue // probe requests are not part of the plan
		}
		req := plan.Requests[i]
		it := plan.Items[req.Item]
		body := r.Normalized
		if body == nil {
			body = r.Raw
		}
		ans, err := answer.Parse(body)
		if err != nil {
			return nil, fmt.Errorf("%s/%s/%s: %w", k.run, k.provider, k.id, err)
		}
		for q, a := range ans {
			probs, _ := json.Marshal(a.Probabilities)
			var score any
			if a.Score != nil {
				score = *a.Score
			}
			if _, err := stAns.Exec(k.run, k.provider, k.id, q, a.Type, a.Choice, score, string(probs)); err != nil {
				return nil, err
			}
			counts["answers"]++
		}
		switch req.Format {
		case "choice":
			a := ans["evaluation"]
			var correctCode string
			for _, c := range it.Candidates {
				if c.Correct && correctCode == "" {
					correctCode = c.Code
				}
			}
			// Items with two correct responses (Ties tied) have no single
			// correct slot; chosen_correct still records whether the pick
			// was a correct response.
			nCorrect := 0
			chosenCorrect := false
			for _, c := range it.Candidates {
				if c.Correct {
					nCorrect++
					if c.Code == a.Choice {
						chosenCorrect = true
					}
				}
			}
			var cs, cq any
			var pc any
			if nCorrect == 1 {
				cs, cq = slotOf(req.StateOrder, correctCode), slotOf(req.QuestionOrder, correctCode)
				if p, ok := a.Probabilities[correctCode]; ok {
					pc = p
				}
			}
			if _, err := stCh.Exec(k.run, k.provider, k.id, it.Key, it.Subset, it.Kind, req.Arm, req.Ordering, req.Repeat, b2i(req.Planted),
				a.Choice, slotOf(req.StateOrder, a.Choice), slotOf(req.QuestionOrder, a.Choice), b2i(chosenCorrect),
				cs, cq, b2i(a.Refused()), pc); err != nil {
				return nil, err
			}
			counts["choice_obs"]++
		case "packed", "solo":
			for _, c := range it.Candidates {
				q := "rate_" + c.Code
				if req.Format == "solo" {
					if req.StateOrder[0] != c.Code {
						continue
					}
					q = "evaluation"
				}
				a, ok := ans[q]
				if !ok {
					return nil, fmt.Errorf("%s/%s/%s: missing answer %q", k.run, k.provider, k.id, q)
				}
				var rating, modal any
				if v, ok := a.Rating(); ok {
					rating = v
				}
				if v, ok := a.ModalRating(); ok {
					modal = v
				}
				if _, err := stRt.Exec(k.run, k.provider, k.id, it.Key, it.Subset, it.Kind, req.Format, req.Arm, req.Ordering, req.Repeat, b2i(req.Planted),
					c.Code, b2i(c.Correct), slotOf(req.StateOrder, c.Code), slotOf(req.QuestionOrder, c.Code), rating, modal, b2i(a.Refused())); err != nil {
					return nil, err
				}
				counts["rating_obs"]++
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if err := db.Close(); err != nil {
		return nil, err
	}
	return counts, os.Rename(tmp, dbPath)
}
