package installer

import (
	"context"
	"strings"
	"testing"
)

func TestSearchFiltersCatalog(t *testing.T) {
	t.Parallel()
	archive := newArchiveServer(t, archiveFiles{
		"skills/index.json":   {body: `{"schema_version":1,"skills":[{"name":"swe","description":"engineering design review"},{"name":"concise-response","description":"writing style"}]}`, mode: 0o644},
		"skills/swe/SKILL.md": {body: "invalid: search must not parse this", mode: 0o644},
	})
	manager := Manager{Client: archive.Client()}

	skills, err := manager.Search(context.Background(), archive.URL, "tag:v1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 || skills[0].Name != "concise-response" {
		t.Fatalf("unexpected catalog: %#v", skills)
	}

	skills, err = manager.Search(context.Background(), archive.URL, "tag:v1", "review")
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].Name != "swe" || skills[0].Path != "skills/swe" {
		t.Fatalf("unexpected description match: %#v", skills)
	}

	skills, err = manager.Search(context.Background(), archive.URL, "tag:v1", "missing")
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 0 {
		t.Fatalf("expected no matches, got %#v", skills)
	}
}

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

func TestSearchRejectsInvalidCatalog(t *testing.T) {
	for _, body := range []string{
		`invalid`,
		`{"schema_version":1,"skills":[{"name":"a","path":"../a","description":"a"}]}`,
		`{"schema_version":1,"skills":[{"name":"a","path":"skills/3rdparty/b","description":"a"}]}`,
		`{"schema_version":1,"skills":[{"name":"a","path":"skills/3rdparty/../a","description":"a"}]}`,
		`{"schema_version":1,"skills":[{"name":"3rdparty","description":"a"}]}`,
		`{"schema_version":2,"skills":[]}`,
		`{"schema_version":1}`,
		`{"schema_version":1,"skills":[{"name":"a"}]}`,
		`{"schema_version":1,"skills":[{"name":"../x","description":"a"}]}`,
		`{"schema_version":1,"skills":[{"name":"a","description":"a"},{"name":"a","description":"b"}]}`,
		`{"schema_version":1,"skills":[{"name":"a/b","description":"a"}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			archive := newArchiveServer(t, archiveFiles{"skills/index.json": {body: body, mode: 0o644}})
			if _, err := (Manager{Client: archive.Client()}).Search(context.Background(), archive.URL, "tag:v1", ""); err == nil {
				t.Fatal("expected invalid catalog error")
			}
		})
	}
	archive := newArchiveServer(t, archiveFiles{"skills/a/SKILL.md": {body: "---\nname: a\ndescription: a\n---", mode: 0o644}})
	if _, err := (Manager{Client: archive.Client()}).Search(context.Background(), archive.URL, "tag:v1", ""); err == nil {
		t.Fatal("missing index must fail without scanning fallback")
	}
}

func TestSearchThirdPartyPaths(t *testing.T) {
	archive := newArchiveServer(t, archiveFiles{
		"skills/index.json": {body: `{"schema_version":1,"skills":[{"name":"external","path":"skills/3rdparty/external","description":"diagram tool"}]}`, mode: 0644},
	})
	for _, query := range []string{"external", "3rdparty", "diagram"} {
		skills, err := (Manager{Client: archive.Client()}).Search(context.Background(), archive.URL, "tag:v1", query)
		if err != nil || len(skills) != 1 || skills[0].Path != "skills/3rdparty/external" {
			t.Fatalf("%s: %+v %v", query, skills, err)
		}
	}
}
