package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

var (
	skillName  = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	skillFlag  = regexp.MustCompile(`^--([a-z][a-z-]*)`)
	inlineCode = regexp.MustCompile("`([^`]+)`")
	usageFlag  = regexp.MustCompile(`(?m)^ +-([a-z][a-z-]*)`)
)

func TestSkillFollowsTheAgentSkillsSpec(t *testing.T) {
	t.Parallel()
	dir, front, body := readSkill(t)
	fields := map[string]string{}
	for line := range strings.SplitSeq(front, "\n") {
		key, value, _ := strings.Cut(line, ": ")
		fields[key] = value
	}
	if name := fields["name"]; len(name) > 64 || !skillName.MatchString(name) || name != filepath.Base(dir) {
		t.Errorf("name %q must be 1 to 64 lowercase letters, digits and single hyphens and match the directory", name)
	}
	if n := utf8.RuneCountInString(fields["description"]); n < 1 || n > 1024 {
		t.Errorf("description has %d characters, want 1 to 1024", n)
	}
	if n := utf8.RuneCountInString(fields["compatibility"]); n > 500 {
		t.Errorf("compatibility has %d characters, want at most 500", n)
	}
	if n := strings.Count(body, "\n"); n >= 500 {
		t.Errorf("the body has %d lines, want fewer than 500", n)
	}
}

func TestSkillUsesOnlyCommandsAndFlagsTheCLIHas(t *testing.T) {
	t.Parallel()
	_, _, body := readSkill(t)
	known := map[string]map[string]bool{}
	every := map[string]bool{}
	for _, c := range commands() {
		if c.name == "serve" || c.name == "sweep-once" || c.name == "takedown" {
			continue
		}
		res := runWith(t, "", nil, nil, c.name, "-h")
		known[c.name] = map[string]bool{}
		for _, m := range usageFlag.FindAllStringSubmatch(res.stdout+res.stderr, -1) {
			known[c.name][m[1]] = true
			every[m[1]] = true
		}
	}
	var commandLines, flagMentions []string
	inBlock := false
	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(line, "```") {
			inBlock = !inBlock
			continue
		}
		if inBlock {
			if strings.HasPrefix(line, "nylzo ") {
				commandLines = append(commandLines, line)
			}
			continue
		}
		for _, m := range inlineCode.FindAllStringSubmatch(line, -1) {
			switch {
			case strings.HasPrefix(m[1], "nylzo "):
				commandLines = append(commandLines, m[1])
			case strings.HasPrefix(m[1], "--"):
				flagMentions = append(flagMentions, m[1])
			}
		}
	}
	if len(commandLines) < 8 || len(flagMentions) < 8 {
		t.Fatalf("found %d commands and %d flag mentions in the skill, expected more", len(commandLines), len(flagMentions))
	}
	for _, line := range commandLines {
		words := strings.Fields(line)
		flags, ok := known[words[1]]
		if !ok {
			t.Errorf("%q: nylzo has no client command %q", line, words[1])
			continue
		}
		for _, word := range words[2:] {
			if m := skillFlag.FindStringSubmatch(word); m != nil && !flags[m[1]] {
				t.Errorf("%q: nylzo %s has no flag --%s", line, words[1], m[1])
			}
		}
		if slices.ContainsFunc(words, fragmentLike.MatchString) {
			t.Errorf("%q puts a link in an argument", line)
		}
	}
	for _, mention := range flagMentions {
		if m := skillFlag.FindStringSubmatch(mention); m != nil && !every[m[1]] {
			t.Errorf("the skill mentions %q, which no nylzo command has", mention)
		}
	}
}

func readSkill(t *testing.T) (dir, front, body string) {
	t.Helper()
	dir = filepath.Join("..", "..", "skills", "nylzo")
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	rest, ok := strings.CutPrefix(strings.ReplaceAll(string(data), "\r\n", "\n"), "---\n")
	if !ok {
		t.Fatal("SKILL.md does not start with YAML frontmatter")
	}
	front, body, ok = strings.Cut(rest, "\n---\n")
	if !ok {
		t.Fatal("the frontmatter of SKILL.md does not end")
	}
	return dir, front, body
}
