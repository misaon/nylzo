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
	skillName   = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	skillField  = regexp.MustCompile(`^(name|description|license|compatibility): ([^>|\s].*)$`)
	skillFlag   = regexp.MustCompile(`^--([a-z][a-z-]*)`)
	inlineCode  = regexp.MustCompile("`([^`]+)`")
	usageFlag   = regexp.MustCompile(`(?m)^ +-([a-z][a-z-]*)`)
	skillClient = []string{"create", "read", "status", "cancel", "burn", "report", "verify-client"}
)

func TestSkillFollowsTheAgentSkillsSpec(t *testing.T) {
	t.Parallel()
	dir, front, body := readSkill(t)
	fields := map[string]string{}
	for line := range strings.SplitSeq(front, "\n") {
		m := skillField.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("frontmatter line %q is not a single-line name, description, license or compatibility", line)
		}
		if _, seen := fields[m[1]]; seen {
			t.Fatalf("frontmatter repeats %s", m[1])
		}
		fields[m[1]] = m[2]
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

type skillSection struct {
	heading  string
	commands []string
	flags    []string
}

func TestSkillUsesOnlyCommandsAndFlagsTheCLIHas(t *testing.T) {
	t.Parallel()
	_, _, body := readSkill(t)
	known := map[string]map[string]bool{}
	every := map[string]bool{}
	for _, name := range skillClient {
		res := runWith(t, "", nil, nil, name, "-h")
		known[name] = map[string]bool{}
		for _, m := range usageFlag.FindAllStringSubmatch(res.stdout+res.stderr, -1) {
			known[name][m[1]] = true
			every[m[1]] = true
		}
	}
	used := map[string]bool{}
	sections := []*skillSection{{heading: "(top)"}}
	inBlock := false
	for line := range strings.SplitSeq(body, "\n") {
		section := sections[len(sections)-1]
		switch {
		case strings.HasPrefix(strings.TrimSpace(line), "```"):
			inBlock = !inBlock
		case inBlock:
			section.commands = append(section.commands, checkInvocations(t, line, known, used)...)
		case strings.HasPrefix(line, "## "):
			sections = append(sections, &skillSection{heading: line})
		default:
			for _, m := range inlineCode.FindAllStringSubmatch(line, -1) {
				if strings.HasPrefix(m[1], "--") {
					section.flags = append(section.flags, m[1])
				} else {
					section.commands = append(section.commands, checkInvocations(t, m[1], known, used)...)
				}
			}
		}
	}
	for _, name := range skillClient {
		if !used[name] {
			t.Errorf("the skill never shows nylzo %s", name)
		}
	}
	for _, section := range sections {
		for _, mention := range section.flags {
			m := skillFlag.FindStringSubmatch(mention)
			if m == nil {
				continue
			}
			if len(section.commands) == 0 {
				if !every[m[1]] {
					t.Errorf("%s mentions %q, which no nylzo command has", section.heading, mention)
				}
				continue
			}
			if !slices.ContainsFunc(section.commands, func(name string) bool { return known[name][m[1]] }) {
				t.Errorf("%s mentions %q, which none of its commands %v has", section.heading, mention, section.commands)
			}
		}
	}
}

func checkInvocations(t *testing.T, line string, known map[string]map[string]bool, used map[string]bool) []string {
	t.Helper()
	var names []string
	words := strings.Fields(line)
	for i := 0; i < len(words); i++ {
		if strings.TrimLeft(words[i], "(") != "nylzo" {
			continue
		}
		if i+1 == len(words) {
			break
		}
		name := words[i+1]
		flags, ok := known[name]
		if !ok {
			t.Errorf("%q: nylzo has no client command %q", line, name)
			continue
		}
		names = append(names, name)
		used[name] = true
		for i += 2; i < len(words) && !shellOperator(words[i]); i++ {
			if m := skillFlag.FindStringSubmatch(words[i]); m != nil && !flags[m[1]] {
				t.Errorf("%q: nylzo %s has no flag --%s", line, name, m[1])
			}
			if fragmentLike.MatchString(words[i]) {
				t.Errorf("%q puts a link in an argument", line)
			}
		}
	}
	return names
}

func shellOperator(word string) bool {
	return slices.Contains([]string{"|", "||", "&&", ";", "&"}, word) || strings.HasPrefix(word, "<") || strings.HasPrefix(word, ">") || strings.HasSuffix(word, ";") || strings.HasSuffix(word, ")")
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
