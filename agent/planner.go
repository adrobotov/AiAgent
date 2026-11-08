package agent

import (
	"context"
	"fmt"
	"strings"

	"agent/llm"
)

// plannerSystemPrompt instructs the model to output a numbered plan.
const plannerSystemPrompt = "You are a task planning assistant. Break the user's task " +
	"into a short list of concrete, ordered steps. Respond ONLY with the numbered steps, " +
	"one per line, in the format '1. <step>'. No explanations, no extra text."

// Plan generates an execution plan for the task using the LLM.
func (a *Agent) Plan(ctx context.Context, task string) ([]string, error) {
	resp, err := a.chatWithRetry(ctx, []llm.Message{
		{Role: "system", Content: plannerSystemPrompt},
		{Role: "user", Content: task},
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("plan generation: %w", err)
	}
	a.recordUsage(resp.Usage)
	return parsePlan(resp.Content), nil
}

// parsePlan extracts numbered steps from an LLM response.
func parsePlan(content string) []string {
	var steps []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		steps = append(steps, stripNumbering(line))
	}
	return steps
}

// stripNumbering removes a leading numbering prefix like "1.", "2)", "3 -",
// "4:" when the prefix consists only of digits.
func stripNumbering(line string) string {
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c >= '0' && c <= '9' {
			continue
		}
		if i > 0 && strings.ContainsRune(".):-", rune(c)) {
			return strings.TrimSpace(line[i+1:])
		}
		return line
	}
	return line
}

// ExecutePlanResult holds the outcome of a plan execution.
type ExecutePlanResult struct {
	Steps    []string
	Results  []string // per-step results (parallel to Steps)
	FailedAt int      // index of the failed step, -1 if all succeeded
}

// ExecutePlan runs each step of the plan as an agent request. Each step sees
// the results of the previous steps in its prompt.
func (a *Agent) ExecutePlan(ctx context.Context, steps []string, onProgress func(done, total int, step string)) (*ExecutePlanResult, error) {
	result := &ExecutePlanResult{Steps: steps, FailedAt: -1}

	for i, step := range steps {
		if onProgress != nil {
			onProgress(i, len(steps), step)
		}

		var b strings.Builder
		b.WriteString("You are executing one step of a plan.\n\nTask plan:\n")
		for j, s := range steps {
			marker := " "
			if j == i {
				marker = ">"
			}
			fmt.Fprintf(&b, "%s %d. %s\n", marker, j+1, s)
		}
		if i > 0 {
			b.WriteString("\nResults of previous steps:\n")
			for j := 0; j < i && j < len(result.Results); j++ {
				fmt.Fprintf(&b, "%d. %s\n", j+1, result.Results[j])
			}
		}
		fmt.Fprintf(&b, "\nExecute ONLY step %d now and report its result.", i+1)

		out, err := a.Process(ctx, b.String())
		if err != nil {
			result.FailedAt = i
			result.Results = append(result.Results, "ERROR: "+err.Error())
			return result, fmt.Errorf("step %d failed: %w", i+1, err)
		}
		result.Results = append(result.Results, out)
	}

	if onProgress != nil {
		onProgress(len(steps), len(steps), "")
	}
	return result, nil
}

// teamSystemPrompts are the role prompts for the multi-agent pipeline.
const (
	criticSystemPrompt = "You are a quality reviewer. Review the assistant's answer to the " +
		"user's task. Reply with:\nAPPROVED: <one-sentence justification>\nor\nREVISION NEEDED: " +
		"<what to fix>\nNothing else."
)

// TeamResult holds the multi-agent pipeline output.
type TeamResult struct {
	Plan          []string
	Answer        string
	CriticVerdict string // "APPROVED" or "REVISION NEEDED"
	CriticNotes   string
	Revised       bool // whether the answer was revised after the critic
}

// RunTeam runs the multi-agent pipeline: planner -> executor -> critic.
// When the critic requests a revision, the answer is revised once.
func (a *Agent) RunTeam(ctx context.Context, task string, onProgress func(stage string)) (*TeamResult, error) {
	if onProgress != nil {
		onProgress("planner")
	}

	// 1. Planner: decompose the task.
	steps, err := a.Plan(ctx, task)
	if err != nil {
		return nil, err
	}
	if len(steps) == 0 {
		// Degenerate case — fall back to direct execution.
		steps = []string{task}
	}

	// 2. Executor: run the plan with tools.
	if onProgress != nil {
		onProgress("executor")
	}
	planResult, err := a.ExecutePlan(ctx, steps, func(done, total int, step string) {
		if onProgress != nil && step != "" {
			onProgress(fmt.Sprintf("executor: step %d/%d — %s", done+1, total, step))
		}
	})
	if err != nil && planResult == nil {
		return nil, err
	}

	// Compose the executor's summary.
	var answer strings.Builder
	for i, res := range planResult.Results {
		fmt.Fprintf(&answer, "%d. %s\n→ %s\n\n", i+1, planResult.Steps[i], res)
	}

	// 3. Critic: review the composed answer.
	if onProgress != nil {
		onProgress("critic")
	}
	critResp, err := a.chatWithRetry(ctx, []llm.Message{
		{Role: "system", Content: criticSystemPrompt},
		{Role: "user", Content: fmt.Sprintf("Task: %s\n\nAssistant's answer:\n%s", task, answer.String())},
	}, nil)
	if err != nil {
		// Critic failure is non-fatal — return the executor's answer.
		return &TeamResult{
			Plan:          steps,
			Answer:        answer.String(),
			CriticVerdict: "APPROVED",
			CriticNotes:   fmt.Sprintf("(critic unavailable: %v)", err),
		}, nil
	}
	a.recordUsage(critResp.Usage)

	team := &TeamResult{Plan: steps, Answer: answer.String()}
	team.CriticVerdict, team.CriticNotes = parseCriticVerdict(critResp.Content)

	// 4. Revision (single pass) when the critic requested it.
	if team.CriticVerdict == "REVISION NEEDED" {
		if onProgress != nil {
			onProgress("revision")
		}
		revResp, err := a.chatWithRetry(ctx, []llm.Message{
			{Role: "system", Content: "You are a helpful assistant. Improve the answer according to the reviewer's feedback."},
			{Role: "user", Content: fmt.Sprintf("Task: %s\n\nDraft answer:\n%s\n\nReviewer feedback: %s\n\nProvide the improved final answer only.",
				task, answer.String(), team.CriticNotes)},
		}, nil)
		if err == nil {
			a.recordUsage(revResp.Usage)
			team.Answer = revResp.Content
			team.Revised = true
		}
	}

	return team, nil
}

// parseCriticVerdict extracts the verdict and notes from the critic response.
func parseCriticVerdict(content string) (verdict, notes string) {
	upper := strings.ToUpper(content)
	if strings.Contains(upper, "REVISION NEEDED") {
		idx := strings.Index(upper, "REVISION NEEDED")
		notes := strings.TrimSpace(content[idx+len("REVISION NEEDED"):])
		notes = strings.TrimPrefix(notes, ":")
		return "REVISION NEEDED", strings.TrimSpace(notes)
	}
	if strings.Contains(upper, "APPROVED") {
		idx := strings.Index(upper, "APPROVED")
		notes := strings.TrimSpace(content[idx+len("APPROVED"):])
		notes = strings.TrimPrefix(notes, ":")
		return "APPROVED", strings.TrimSpace(notes)
	}
	return "APPROVED", content
}
