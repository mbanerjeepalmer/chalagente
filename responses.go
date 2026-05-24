package main

import (
	_ "embed"
	"strings"
)

//go:embed docs/v1.5_responses.md
var responsesMD string

type ScriptStep struct {
	Topic    string
	Triggers string
	Reply    string
}

var (
	hardcodedScript    = parseScript(responsesMD)
	hardcodedResponses = replies(hardcodedScript)
)

func replies(steps []ScriptStep) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.Reply
	}
	return out
}

func parseScript(md string) []ScriptStep {
	var out []ScriptStep
	var cur ScriptStep
	flush := func() {
		if cur.Reply != "" {
			out = append(out, cur)
		}
		cur = ScriptStep{}
	}
	for _, line := range strings.Split(md, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "## "):
			flush()
			cur.Topic = strings.TrimSpace(strings.TrimLeft(t, "# "))
			if i := strings.Index(cur.Topic, ". "); i >= 0 {
				cur.Topic = cur.Topic[i+2:]
			}
		case strings.HasPrefix(t, "**Trigger:**"):
			cur.Triggers = strings.TrimSpace(strings.TrimPrefix(t, "**Trigger:**"))
		case strings.HasPrefix(t, ">"):
			body := strings.TrimSpace(strings.TrimPrefix(t, ">"))
			if strings.HasPrefix(body, `"`) {
				cur.Reply = strings.Trim(body, `"`)
			}
		}
	}
	flush()
	return out
}
