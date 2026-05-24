package main

import (
	_ "embed"
	"strings"
)

//go:embed docs/v1.5_responses.md
var responsesMD string

var hardcodedResponses = parseResponses(responsesMD)

func parseResponses(md string) []string {
	var out []string
	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, ">") {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(line, ">"))
		if !strings.HasPrefix(body, `"`) {
			continue
		}
		out = append(out, strings.Trim(body, `"`))
	}
	return out
}
