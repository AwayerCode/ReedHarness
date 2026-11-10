package installer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Every repository Skill installs on its own; the list is the directory scan, so a
// new Skill is covered without editing this test.
func TestInstallIndependentSWESkills(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	entries, err := os.ReadDir(filepath.Join(root, "skills"))
	if err != nil {
		t.Fatal(err)
	}
	skills := []Skill{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, "skills", entry.Name(), "SKILL.md"))
		if err != nil {
			continue
		}
		skill, err := parseSkill(data, "skills/"+entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		skills = append(skills, skill)
	}
	if len(skills) == 0 {
		t.Fatal("no Skill directory found")
	}
	for _, skill := range skills {
		for _, dryRun := range []bool{false, true} {
			files := archiveFiles{skill.Path + "/SKILL.md": {body: skillBody(skill.Name), mode: 0644}, "skills/unrelated/SKILL.md": {body: skillBody("unrelated"), mode: 0644}}
			archive := newArchiveServer(t, files)
			project := t.TempDir()
			manager := Manager{ProjectRoot: project, Client: archive.Client(), DryRun: dryRun}
			results, err := manager.Install(context.Background(), archive.URL, "v3", []string{skill.Path})
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || results[0].Path != ".agents/skills/"+skill.Name {
				t.Fatalf("results: %+v", results)
			}
			if dryRun {
				if _, err := os.Stat(filepath.Join(project, ".agents")); !os.IsNotExist(err) {
					t.Fatalf("dry run wrote files: %v", err)
				}
				continue
			}
			assertFile(t, project, skill.Path+"/SKILL.md", skillBody(skill.Name))
			entries, err := os.ReadDir(filepath.Join(project, ".agents", "skills"))
			if err != nil || len(entries) != 1 {
				t.Fatalf("unexpected installed skills: %v %v", entries, err)
			}
		}
	}
}

func TestLegacySWEPathDoesNotExpandSiblings(t *testing.T) {
	files := archiveFiles{"skills/swe/SKILL.md": {body: skillBody("swe"), mode: 0644}, "skills/swe-testing/SKILL.md": {body: skillBody("swe-testing"), mode: 0644}}
	archive := newArchiveServer(t, files)
	manager := Manager{ProjectRoot: t.TempDir(), Client: archive.Client()}
	results, err := manager.Install(context.Background(), archive.URL, "v3", []string{"skills/swe"})
	if err != nil || len(results) != 1 {
		t.Fatalf("results: %v %v", results, err)
	}
}
