package installer

import (
	"strings"
	"testing"
)

func TestParseSkill(t *testing.T) {
	tests := []struct{ name, data, want string }{
		{"missing front matter", "hello", "missing YAML"},
		{"unclosed", "---\nname: swe", "unclosed"},
		{"missing description", "---\nname: swe\n---", "requires name and description"},
		{"invalid YAML", "---\nname: [\n---", "parse YAML"},
		{"duplicate key", "---\nname: swe\nname: other\ndescription: hi\n---", "parse YAML"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseSkill([]byte(test.data), "skills/swe")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want %s", err, test.want)
			}
		})
	}
	skill, err := parseSkill([]byte("\xef\xbb\xbf---\r\nname: swe\r\ndescription: \"design: review\"\r\n---\r\n"), "skills/swe")
	if err != nil || skill.Summary != "design: review" {
		t.Fatalf("%#v: %v", skill, err)
	}
}
