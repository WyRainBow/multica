package main

import (
	"fmt"
	"io"
	"strings"
)

// The nudge that turns finishing into learning.
//
// The loop the platform is built around ends with experience feeding the next
// round: an issue closes, the run is reconstructed, what was learned becomes a
// case, and the next issue starts from it. The skill that does that
// reconstruction exists and says when to use it. Nothing says it at the moment
// it applies — and the evidence is blunt: every case in this workspace was
// written by hand, none through the skill.
//
// So this prints a line and stops. It does not run the retro, and that is the
// whole design: a closing action that started summoning agents would be a
// pipeline, and the discipline here has been that assets are offered, never
// forced. A line a person ignores costs nothing; a step they cannot skip gets
// worked around, and then the rule is worse than absent because people believe
// it is running.

// retroTriggerStatuses are the issue states that mean the work is over.
// `blocked` is deliberately absent: a blocked issue has learned something, but
// it is not finished learning it, and the retro would summarise a middle.
var retroTriggerStatuses = map[string]bool{
	"done":      true,
	"cancelled": true,
}

// retroFinalPhases name the stations that end a route. Matched by prefix
// because a station can carry a round number, and cancelled work reaches
// neither — this is only about a route that ran to its end.
var retroFinalPhases = []string{"需求冻结", "测试验收"}

// shouldPromptRetro reports whether finishing at this point is worth a nudge.
func shouldPromptRetro(status, phase string) bool {
	if retroTriggerStatuses[strings.ToLower(strings.TrimSpace(status))] {
		return true
	}
	trimmed := strings.TrimSpace(phase)
	for _, final := range retroFinalPhases {
		if strings.HasPrefix(trimmed, final) {
			return true
		}
	}
	return false
}

// writeRetroPrompt emits the nudge. One line, on stderr, so it never
// contaminates the JSON a caller may be parsing.
//
// Naming the command matters more than naming the skill: an agent reading
// "consider a retro" has to work out what that means, while a command it can
// run is a decision it can take in one step.
func writeRetroPrompt(w io.Writer, key, reason string) {
	fmt.Fprintf(w, "\n%s 到此为止了（%s）。这一轮学到的东西现在不写下来，下一张卡就要重新学一遍。\n", key, reason)
	fmt.Fprintf(w, "  想沉淀就走 `interview-retro` skill：读本卡的 issue、评论、轮次文档与运行记录，还原这次是怎么走的，够格的写成 AgentWiki case。\n")
	fmt.Fprintf(w, "  不想就不写——这只是一句提醒，没有任何东西在等它。\n")
}
