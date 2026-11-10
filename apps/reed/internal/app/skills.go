package app

import (
	"fmt"
	"io"

	"github.com/AwayerCode/ReedHarness/apps/reed/internal/skills"
)

func Skills(args []string, out io.Writer) int {
	if len(args) == 0 || len(args) == 1 && helpFlag(args[0]) ||
		len(args) > 1 && (args[0] == "list" || args[0] == "read") && helpFlag(args[len(args)-1]) {
		return printHelp(out, "Usage: reed skills list\n       reed skills read <name> [relative-resource-path]\nread defaults to SKILL.md and writes the compiled text to stdout.")
	}
	if len(args) == 1 && args[0] == "list" {
		return encode(out, map[string]any{"version": Version, "skills": skills.Names()}, 0)
	}
	if (len(args) == 2 || len(args) == 3) && args[0] == "read" {
		path := "SKILL.md"
		if len(args) == 3 {
			path = args[2]
		}
		content, err := skills.Read(args[1], path)
		if err != nil {
			return unavailable(out, fmt.Errorf("Skill resource: %w", err))
		}
		if _, err := io.WriteString(out, content); err != nil {
			return 2
		}
		return 0
	}
	return unavailable(out, fmt.Errorf("expected skills list or skills read <name> [relative-resource-path]"))
}
