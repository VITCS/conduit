package components

// Agent questions. A planning or coding step may stop and ask the person a
// question instead of guessing, with a few suggested answers. The step returns
// OutcomeQuestion; the engine pauses the run, shows the question (in the run and
// in the "Needs you" list), and re-runs the step with the answer.
//
// The model asks by replying with only:
//
//	<question>Which auth provider should the login page use?</question>
//	<option>Keep the existing email login</option>
//	<option>Add Google sign-in</option>
//
// Answers are kept in run memory under answers_for_<node> and given back to the
// step on every later run, so it never asks the same thing twice. A step may ask
// at most max_questions times (default 2); ask=false on a node, or the project
// variable agent_questions=off, turns asking off.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"conduit/internal/model"
)

// OutcomeQuestion is the outcome of a step that stopped to ask the person something.
const OutcomeQuestion model.Status = "question"

const defaultMaxQuestions = 2

var (
	questionRe = regexp.MustCompile(`(?s)<question>(.*?)</question>`)
	optionRe   = regexp.MustCompile(`(?s)<option>(.*?)</option>`)
)

// QA is one question a step asked and the person's answer.
type QA struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// Answers returns the questions a step has asked in this run and their answers.
func Answers(run *model.Run, nodeID string) []QA {
	if run == nil {
		return nil
	}
	raw, _ := run.Memory["answers_for_"+nodeID].([]any)
	out := make([]QA, 0, len(raw))
	for _, v := range raw {
		switch m := v.(type) {
		case map[string]any:
			q, _ := m["question"].(string)
			a, _ := m["answer"].(string)
			out = append(out, QA{q, a})
		case QA:
			out = append(out, m)
		}
	}
	return out
}

// questionsLeft is how many more questions this step may ask (0 = asking is off).
func questionsLeft(n model.Node, run *model.Run) int {
	if strings.EqualFold(n.Field("ask"), "false") || strings.EqualFold(projVar(runProject(run), "agent_questions"), "off") {
		return 0
	}
	max := defaultMaxQuestions
	if v, err := strconv.Atoi(strings.TrimSpace(n.Field("max_questions"))); err == nil && v >= 0 {
		max = v
	}
	left := max - len(Answers(run, n.ID))
	if left < 0 {
		return 0
	}
	return left
}

// askBlock is added to a prompt: the answers so far, and (while questions are
// left) how to ask one. answerFormat names what the step normally returns.
func askBlock(n model.Node, run *model.Run, answerFormat string) string {
	var b strings.Builder
	if qa := Answers(run, n.ID); len(qa) > 0 {
		b.WriteString("\n\n--- ANSWERS FROM THE PERSON (follow them; do not ask these again) ---\n")
		for _, x := range qa {
			fmt.Fprintf(&b, "Q: %s\nA: %s\n", x.Question, x.Answer)
		}
	}
	left := questionsLeft(n, run)
	if left == 0 {
		return b.String()
	}
	fmt.Fprintf(&b, `

--- IF YOU NEED A DECISION ---
If, and only if, you cannot do this well without a decision only the person can make
(an ambiguous requirement, or a choice between real alternatives), do not guess.
Instead of %s, reply with ONLY:
<question>one short, specific question</question>
<option>a suggested answer</option>
(2 to 4 options, each a few words). Otherwise, do the task. You may ask %d more question(s).`, answerFormat, left)
	return b.String()
}

// parseQuestion finds a question in a model reply.
func parseQuestion(text string) (string, []string, bool) {
	m := questionRe.FindStringSubmatch(text)
	if m == nil {
		return "", nil, false
	}
	q := strings.TrimSpace(m[1])
	if q == "" {
		return "", nil, false
	}
	if len(q) > 600 {
		q = q[:600] + "…"
	}
	var opts []string
	for _, o := range optionRe.FindAllStringSubmatch(text, -1) {
		if s := strings.TrimSpace(o[1]); s != "" && len(opts) < 4 {
			if len(s) > 160 {
				s = s[:160] + "…"
			}
			opts = append(opts, s)
		}
	}
	return q, opts, true
}

// questionResult is what a step returns when it asks.
func questionResult(n model.Node, run *model.Run, q string, opts []string) Result {
	return Result{Outcome: OutcomeQuestion,
		Output: map[string]any{"question": q, "options": opts, "questions_left": questionsLeft(n, run) - 1},
		Logs:   []string{"asked: " + q}}
}

// maybeQuestion turns a reply that asks a question into a question Result, when
// this step may still ask.
func maybeQuestion(n model.Node, run *model.Run, text string) (Result, bool) {
	if questionsLeft(n, run) == 0 {
		return Result{}, false
	}
	q, opts, ok := parseQuestion(text)
	if !ok {
		return Result{}, false
	}
	return questionResult(n, run, q, opts), true
}
