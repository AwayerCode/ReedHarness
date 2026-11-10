package systemdesign

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var cppExtensions = map[string]bool{
	".c": true, ".cc": true, ".cpp": true, ".cxx": true,
	".h": true, ".hh": true, ".hpp": true, ".hxx": true,
	".ipp": true, ".inl": true,
}

var cppIncludeDirective = regexp.MustCompile(`^\s*#\s*include\b(.*)$`)

type cppSourceAdapter struct{}

func (cppSourceAdapter) Name() string { return "C++" }

func (cppSourceAdapter) Matches(inventory sourceInventory) bool {
	for _, file := range inventory.Files {
		if isCppSource(file.Relative) {
			return true
		}
	}
	return false
}

func isCppSource(rel string) bool {
	return cppExtensions[strings.ToLower(filepath.Ext(rel))]
}

func (cppSourceAdapter) Check(d *Design, inventory sourceInventory, r *Report, _ *Session, state *sourceAdapterState) error {
	includeDirs, missingRoots, stringCommands, err := cppIncludeDirectories(inventory.Root)
	if err != nil {
		return err
	}
	files, references, unresolvedExternal := 0, 0, 0
	for _, file := range inventory.Files {
		if !isCppSource(file.Relative) {
			continue
		}
		data, err := readLanguageSource(file.Path)
		if err != nil {
			return err
		}
		includes, err := readCppIncludes(data)
		if err != nil {
			return fmt.Errorf("%s: %w", file.Relative, err)
		}
		for _, include := range includes {
			references++
			candidates := cppIncludeCandidates(inventory.Root, d.Source, file, include, includeDirs)
			matches, err := existingSourceFiles(inventory.Root, candidates)
			if err != nil {
				return err
			}
			switch len(matches) {
			case 0:
				if include.Quoted && (strings.HasPrefix(include.Path, ".") || strings.HasPrefix(include.Path, "/")) {
					addMissingLanguageReference(r, file.Relative, "C++", include.Raw(), include.Line)
				} else {
					unresolvedExternal++
				}
			case 1:
				if err := d.checkResolvedSourceFile(inventory, file, matches[0], include.Raw(), include.Line, "C++", state, r); err != nil {
					return err
				}
			default:
				paths := make([]string, len(matches))
				for i, match := range matches {
					rel, _ := filepath.Rel(inventory.Root, match)
					paths[i] = filepath.ToSlash(rel)
				}
				return fmt.Errorf("ambiguous C++ include %s: %s", include.Raw(), strings.Join(paths, ", "))
			}
		}
		files++
	}
	r.Coverage = append(r.Coverage, fmt.Sprintf("%d C++ source/header files parsed; %d include directives inspected", files, references), "Quoted includes, source-root includes and compile_commands.json -I/-isystem include directories")
	if unresolvedExternal > 0 {
		r.Unverified = append(r.Unverified, fmt.Sprintf("%d C++ includes did not resolve to managed source and were treated as external.", unresolvedExternal))
	}
	if missingRoots > 0 {
		r.Unverified = append(r.Unverified, fmt.Sprintf("%d compile_commands.json include directories were absent.", missingRoots))
	}
	if stringCommands > 0 {
		r.Unverified = append(r.Unverified, fmt.Sprintf("%d compile_commands.json entries only provided shell command strings; their include paths were not parsed.", stringCommands))
	}
	r.Unverified = append(r.Unverified, "Preprocessor conditionals and compiler-generated or macro-expanded declarations are not evaluated; macro include operands are rejected.")
	return nil
}

type cppInclude struct {
	Path   string
	Quoted bool
	Line   int
}

func (include cppInclude) Raw() string {
	if include.Quoted {
		return `"` + include.Path + `"`
	}
	return "<" + include.Path + ">"
}

func readCppIncludes(data []byte) ([]cppInclude, error) {
	lines := strings.Split(string(data), "\n")
	includes := []cppInclude{}
	inBlock := false
	for index, original := range lines {
		line := stripCppComments(original, &inBlock)
		match := cppIncludeDirective.FindStringSubmatch(strings.TrimPrefix(line, "\ufeff"))
		if match == nil {
			continue
		}
		rest := strings.TrimSpace(match[1])
		if rest == "" {
			return nil, fmt.Errorf("line %d: empty include directive", index+1)
		}
		opening := rest[0]
		closing := byte('>')
		quoted := opening == '"'
		if quoted {
			closing = '"'
		} else if opening != '<' {
			return nil, fmt.Errorf("line %d: macro-expanded C++ includes are unsupported", index+1)
		}
		end := strings.IndexByte(rest[1:], closing)
		if end < 0 {
			return nil, fmt.Errorf("line %d: malformed include directive", index+1)
		}
		end++
		includePath := strings.TrimSpace(rest[1:end])
		trailing := strings.TrimSpace(rest[end+1:])
		if includePath == "" || trailing != "" || strings.ContainsRune(includePath, '\x00') {
			return nil, fmt.Errorf("line %d: malformed include directive", index+1)
		}
		includePath = strings.ReplaceAll(includePath, "\\", "/")
		if strings.HasPrefix(includePath, "/") || strings.Contains(includePath, ":") {
			return nil, fmt.Errorf("line %d: absolute include path is unsupported: %s", index+1, includePath)
		}
		includes = append(includes, cppInclude{Path: includePath, Quoted: quoted, Line: index + 1})
	}
	return includes, nil
}

func stripCppComments(line string, inBlock *bool) string {
	var result strings.Builder
	inString, escaped := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if *inBlock {
			if c == '*' && i+1 < len(line) && line[i+1] == '/' {
				result.WriteString("  ")
				i++
				*inBlock = false
			} else {
				result.WriteByte(' ')
			}
			continue
		}
		if inString {
			result.WriteByte(c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			result.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(line) && line[i+1] == '/' {
			result.WriteString(strings.Repeat(" ", len(line)-i))
			break
		}
		if c == '/' && i+1 < len(line) && line[i+1] == '*' {
			result.WriteString("  ")
			i++
			*inBlock = true
			continue
		}
		result.WriteByte(c)
	}
	return result.String()
}

func cppIncludeCandidates(root, sourceRoot string, file sourceFile, include cppInclude, includeDirs []string) []string {
	candidates := []string{}
	if include.Quoted {
		candidates = append(candidates, filepath.Join(filepath.Dir(file.Path), filepath.FromSlash(include.Path)))
	}
	for _, dir := range includeDirs {
		candidates = append(candidates, filepath.Join(dir, filepath.FromSlash(include.Path)))
	}
	candidates = append(candidates, filepath.Join(root, filepath.FromSlash(sourceRoot), filepath.FromSlash(include.Path)))
	return candidates
}

type cppCompileCommand struct {
	Directory string   `json:"directory"`
	Arguments []string `json:"arguments"`
	Command   string   `json:"command"`
}

func cppIncludeDirectories(root string) ([]string, int, int, error) {
	database := filepath.Join(root, "compile_commands.json")
	info, err := regularPath(root, "compile_commands.json")
	if os.IsNotExist(err) {
		return nil, 0, 0, nil
	}
	if err != nil {
		return nil, 0, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, 0, fmt.Errorf("compile_commands.json is not a regular file")
	}
	data, err := os.ReadFile(database)
	if err != nil {
		return nil, 0, 0, err
	}
	var commands []cppCompileCommand
	if err := json.Unmarshal(data, &commands); err != nil {
		return nil, 0, 0, fmt.Errorf("parse compile_commands.json: %w", err)
	}
	seen := map[string]bool{}
	dirs := []string{}
	missing, stringCommands := 0, 0
	add := func(command cppCompileCommand, dir string) {
		if dir == "" {
			return
		}
		if !filepath.IsAbs(dir) {
			base := command.Directory
			if base == "" {
				base = root
			}
			dir = filepath.Join(base, dir)
		}
		absolute, err := filepath.Abs(dir)
		if err != nil {
			missing++
			return
		}
		dir, err = filepath.EvalSymlinks(absolute)
		if err != nil {
			missing++
			return
		}
		rel, err := filepath.Rel(root, dir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return
		}
		if seen[dir] {
			return
		}
		seen[dir] = true
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			missing++
			return
		}
		dirs = append(dirs, dir)
	}
	for _, command := range commands {
		if len(command.Arguments) == 0 {
			if strings.TrimSpace(command.Command) != "" {
				stringCommands++
			}
			continue
		}
		args := command.Arguments
		for i := 0; i < len(args); i++ {
			arg := args[i]
			switch {
			case arg == "-I" || arg == "-isystem" || arg == "/I":
				if i+1 < len(args) {
					i++
					add(command, args[i])
				}
			case strings.HasPrefix(arg, "-isystem") && len(arg) > len("-isystem"):
				add(command, strings.TrimPrefix(arg, "-isystem"))
			case strings.HasPrefix(arg, "-I") && len(arg) > 2:
				add(command, arg[2:])
			case strings.HasPrefix(arg, "/I") && len(arg) > 2:
				add(command, arg[2:])
			}
		}
	}
	sort.Strings(dirs)
	return dirs, missing, stringCommands, nil
}
