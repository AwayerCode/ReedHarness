package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/AwayerCode/ReedHarness/apps/reed/internal/skills"
)

func TestSkillCommands(t *testing.T) {
	var out bytes.Buffer
	if code := Run([]string{"skills", "list"}, nil, &out); code != 0 {
		t.Fatal(code, out.String())
	}
	var list struct {
		Version string
		Skills  []string
	}
	if err := json.Unmarshal(out.Bytes(), &list); err != nil || list.Version != Version || len(list.Skills) == 0 {
		t.Fatal(list, err)
	}
	for _, name := range list.Skills {
		out.Reset()
		if code := Run([]string{"skills", "read", name}, nil, &out); code != 0 {
			t.Fatal(code, out.String())
		}
		want, err := skills.Read(name, "SKILL.md")
		if err != nil || out.String() != want {
			t.Fatal(name, err)
		}
	}
	out.Reset()
	if code := Run([]string{"skills", "read", "swe-testing", "references/cli.md"}, nil, &out); code != 0 {
		t.Fatal(code, out.String())
	}
	for _, args := range [][]string{{"skills", "read"}, {"skills", "list", "extra"}, {"skills", "read", "swe-testing", "../SKILL.md"}, {"skills", "read", "missing"}, {"install"}, {"list"}} {
		out.Reset()
		if code := Run(args, nil, &out); code != 2 {
			t.Fatal(args, code, out.String())
		}
	}
	if code := Run([]string{"skills", "read", "swe-testing"}, nil, failedSkillWriter{}); code != 2 {
		t.Fatal(code)
	}
}

type failedSkillWriter struct{}

func (failedSkillWriter) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }

func TestSkillCommandHelp(t *testing.T) {
	for _, command := range [][]string{{"skills"}, {"skills", "list"}, {"skills", "read"}, {"skills", "read", "swe-testing"}} {
		for _, flag := range []string{"-h", "--help"} {
			args := append(append([]string{}, command...), flag)
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				var out bytes.Buffer
				if code := Run(args, nil, &out); code != 0 || !strings.Contains(out.String(), "Usage: reed skills") {
					t.Fatalf("%d: %s", code, &out)
				}
			})
		}
		args := append([]string{"help"}, command...)
		var out bytes.Buffer
		if code := Run(args, nil, &out); code != 0 || !strings.Contains(out.String(), "Usage: reed skills") {
			t.Fatalf("%v: %d: %s", args, code, &out)
		}
	}
}
