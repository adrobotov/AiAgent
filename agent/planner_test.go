package agent

import (
	"context"
	"strings"
	"testing"
)

func TestParsePlan(t *testing.T) {
	content := "1. Research the topic\n2. Write the draft\n3. Review and finalize"
	steps := parsePlan(content)
	if len(steps) != 3 {
		t.Fatalf("got %d steps: %v", len(steps), steps)
	}
	if steps[0] != "Research the topic" || steps[2] != "Review and finalize" {
		t.Fatalf("unexpected steps: %v", steps)
	}
}

func TestParsePlanVariants(t *testing.T) {
	steps := parsePlan("1) step one\n2- step two\nplain line\n\n3. step three")
	if len(steps) != 4 {
		t.Fatalf("got %d steps: %v", len(steps), steps)
	}
	if steps[0] != "step one" || steps[1] != "step two" || steps[2] != "plain line" || steps[3] != "step three" {
		t.Fatalf("unexpected steps: %v", steps)
	}
}

func TestAgentPlan(t *testing.T) {
	ag, _ := newTestAgent(t, []mockResp{
		{JSON: plainResp("1. Do the first thing\n2. Do the second thing")},
	})

	steps, err := ag.Plan(context.Background(), "make something")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("got %d steps: %v", len(steps), steps)
	}
}

func TestAgentExecutePlan(t *testing.T) {
	ag, lastMsgs := newTestAgent(t, []mockResp{
		{JSON: plainResp("1. Step A\n2. Step B")},
		{JSON: plainResp("result A")},
		{JSON: plainResp("result B")},
	})

	steps, err := ag.Plan(context.Background(), "task")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	var progressLog []string
	result, err := ag.ExecutePlan(context.Background(), steps, func(done, total int, step string) {
		progressLog = append(progressLog, step)
	})
	if err != nil {
		t.Fatalf("ExecutePlan: %v", err)
	}
	if result.FailedAt != -1 {
		t.Fatalf("unexpected failure at %d", result.FailedAt)
	}
	if len(result.Results) != 2 || result.Results[0] != "result A" || result.Results[1] != "result B" {
		t.Fatalf("unexpected results: %v", result.Results)
	}
	if len(progressLog) < 2 {
		t.Fatalf("progress not reported: %v", progressLog)
	}

	// The second step's request must mention the first step's result.
	found := false
	for _, m := range *lastMsgs {
		if strings.Contains(m.Content, "result A") {
			found = true
		}
	}
	if !found {
		t.Fatal("step 2 request must contain step 1 result")
	}
}

func TestAgentRunTeamApproved(t *testing.T) {
	ag, _ := newTestAgent(t, []mockResp{
		{JSON: plainResp("1. Gather facts\n2. Write answer")},
		{JSON: plainResp("facts collected")},
		{JSON: plainResp("answer draft")},
		{JSON: plainResp("APPROVED: answer is correct")},
	})

	team, err := ag.RunTeam(context.Background(), "solve it", func(stage string) {})
	if err != nil {
		t.Fatalf("RunTeam: %v", err)
	}
	if len(team.Plan) != 2 {
		t.Fatalf("unexpected plan: %v", team.Plan)
	}
	if team.CriticVerdict != "APPROVED" {
		t.Fatalf("verdict = %s", team.CriticVerdict)
	}
	if team.Revised {
		t.Fatal("must not be revised on APPROVED")
	}
	if !strings.Contains(team.Answer, "facts collected") || !strings.Contains(team.Answer, "answer draft") {
		t.Fatalf("answer missing step results: %q", team.Answer)
	}
}

func TestAgentRunTeamRevision(t *testing.T) {
	ag, _ := newTestAgent(t, []mockResp{
		{JSON: plainResp("1. Only step")},
		{JSON: plainResp("weak answer")},
		{JSON: plainResp("REVISION NEEDED: add more details")},
		{JSON: plainResp("improved final answer")},
	})

	team, err := ag.RunTeam(context.Background(), "task", func(stage string) {})
	if err != nil {
		t.Fatalf("RunTeam: %v", err)
	}
	if team.CriticVerdict != "REVISION NEEDED" {
		t.Fatalf("verdict = %s", team.CriticVerdict)
	}
	if !team.Revised {
		t.Fatal("expected revision")
	}
	if team.Answer != "improved final answer" {
		t.Fatalf("unexpected revised answer: %q", team.Answer)
	}
}

func TestAgentRunTeamCriticFailureNonFatal(t *testing.T) {
	// Critic response is missing (mock exhausted) — pipeline must still
	// return the executor's answer.
	ag, _ := newTestAgent(t, []mockResp{
		{JSON: plainResp("1. Only step")},
		{JSON: plainResp("step done")},
	})
	ag.retryCount = 0 // avoid multi-second retry delays in tests

	team, err := ag.RunTeam(context.Background(), "task", func(stage string) {})
	if err != nil {
		t.Fatalf("RunTeam: %v", err)
	}
	if team.CriticVerdict != "APPROVED" {
		t.Fatalf("verdict = %s", team.CriticVerdict)
	}
	if team.CriticNotes == "" {
		t.Fatal("critic notes must mention unavailability")
	}
}
