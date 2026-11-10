package systemdesign

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

var typeScriptExtensions = map[string]bool{
	".ts": true, ".tsx": true, ".mts": true, ".cts": true,
}

type typeScriptSourceAdapter struct{}

func (typeScriptSourceAdapter) Name() string { return "TypeScript" }

func (typeScriptSourceAdapter) Matches(inventory sourceInventory) bool {
	for _, file := range inventory.Files {
		if isTypeScriptSource(file.Relative) {
			return true
		}
	}
	return false
}

func isTypeScriptSource(rel string) bool {
	return typeScriptExtensions[strings.ToLower(filepath.Ext(rel))]
}

func (typeScriptSourceAdapter) Check(d *Design, inventory sourceInventory, r *Report, _ *Session, state *sourceAdapterState) error {
	resolver := newTypeScriptResolver(inventory.Root)
	files, references, unmappedBare, unresolvedDynamic := 0, 0, 0, 0
	for _, file := range inventory.Files {
		if !isTypeScriptSource(file.Relative) {
			continue
		}
		data, err := readLanguageSource(file.Path)
		if err != nil {
			return err
		}
		found, dynamic, err := readTypeScriptReferences(data)
		if err != nil {
			return fmt.Errorf("%s: %w", file.Relative, err)
		}
		unresolvedDynamic += dynamic
		for _, ref := range found {
			references++
			resolution, err := resolver.resolve(file, ref.Specifier)
			if err != nil {
				return fmt.Errorf("%s:%d: %w", file.Relative, ref.Line, err)
			}
			switch resolution.kind {
			case typeScriptResolutionFile:
				if err := d.checkResolvedSourceFile(inventory, file, resolution.target, ref.Specifier, ref.Line, "TypeScript", state, r); err != nil {
					return err
				}
			case typeScriptResolutionMissing:
				addMissingLanguageReference(r, file.Relative, "TypeScript", ref.Specifier, ref.Line)
			case typeScriptResolutionExternal:
				if !strings.HasPrefix(ref.Specifier, ".") {
					unmappedBare++
				}
			}
		}
		files++
	}
	r.Coverage = append(r.Coverage, fmt.Sprintf("%d TypeScript files parsed; %d static import/export/require references resolved", files, references), "Relative references plus nearest tsconfig.json baseUrl/paths; external package references excluded")
	if unmappedBare > 0 {
		r.Unverified = append(r.Unverified, fmt.Sprintf("%d bare TypeScript imports were treated as external because no local tsconfig path matched.", unmappedBare))
	}
	if unresolvedDynamic > 0 {
		r.Unverified = append(r.Unverified, fmt.Sprintf("%d non-literal dynamic TypeScript imports were not resolved.", unresolvedDynamic))
	}
	if resolver.incomplete {
		r.Unverified = append(r.Unverified, "At least one tsconfig.json uses extends; inherited path aliases were not evaluated.")
	}
	r.Unverified = append(r.Unverified, "TypeScript type checking, package.json exports/imports mapping and inherited package tsconfig resolution are not performed.")
	return nil
}

type typeScriptReference struct {
	Specifier string
	Line      int
}

type tsTokenKind uint8

const (
	tsIdentifier tsTokenKind = iota
	tsString
	tsPunct
)

type tsToken struct {
	Kind tsTokenKind
	Text string
	Line int
}

func readTypeScriptReferences(data []byte) ([]typeScriptReference, int, error) {
	tokens, err := tokenizeTypeScript(data)
	if err != nil {
		return nil, 0, err
	}
	seen := map[string]bool{}
	references := []typeScriptReference{}
	dynamic := 0
	add := func(specifier string, line int) {
		key := fmt.Sprintf("%d:%s", line, specifier)
		if specifier == "" || seen[key] {
			return
		}
		seen[key] = true
		references = append(references, typeScriptReference{Specifier: specifier, Line: line})
	}
	for i := 0; i < len(tokens); i++ {
		if tokens[i].Kind != tsIdentifier {
			continue
		}
		switch tokens[i].Text {
		case "import":
			if i > 0 && tokens[i-1].Text == "." {
				continue
			}
			if i+1 < len(tokens) && tokens[i+1].Text == "." {
				continue
			}
			if i+1 < len(tokens) && tokens[i+1].Text == "(" {
				line := tokens[i].Line
				if i+2 < len(tokens) && tokens[i+2].Kind == tsString {
					add(tokens[i+2].Text, line)
				} else {
					dynamic++
				}
				continue
			}
			if i+1 < len(tokens) && tokens[i+1].Kind == tsString {
				add(tokens[i+1].Text, tokens[i].Line)
				continue
			}
			if specifier, ok := tsImportFromSpecifier(tokens, i+1); ok {
				add(specifier, tokens[i].Line)
			}
		case "export":
			if specifier, ok := tsExportFromSpecifier(tokens, i+1); ok {
				add(specifier, tokens[i].Line)
			}
		case "require":
			if (i == 0 || tokens[i-1].Text != ".") &&
				i+3 < len(tokens) && tokens[i+1].Text == "(" && tokens[i+2].Kind == tsString {
				add(tokens[i+2].Text, tokens[i].Line)
			}
		}
	}
	return references, dynamic, nil
}

func tsImportFromSpecifier(tokens []tsToken, start int) (string, bool) {
	depth := 0
	for i := start; i < len(tokens); i++ {
		if tokens[i].Text == ";" || (i > start && tokens[i].Kind == tsIdentifier && (tokens[i].Text == "import" || tokens[i].Text == "export")) {
			return "", false
		}
		if tokens[i].Text == "{" {
			depth++
			continue
		}
		if tokens[i].Text == "}" {
			depth--
			continue
		}
		if depth == 0 && tokens[i].Kind == tsIdentifier && tokens[i].Text == "from" && i+1 < len(tokens) && tokens[i+1].Kind == tsString {
			return tokens[i+1].Text, true
		}
	}
	return "", false
}

func tsExportFromSpecifier(tokens []tsToken, start int) (string, bool) {
	i := start
	if i < len(tokens) && tokens[i].Kind == tsIdentifier && tokens[i].Text == "type" {
		i++
	}
	if i >= len(tokens) || tokens[i].Text != "{" && tokens[i].Text != "*" {
		return "", false
	}
	return tsImportFromSpecifier(tokens, start)
}

func tokenizeTypeScript(data []byte) ([]tsToken, error) {
	tokens := []tsToken{}
	line := 1
	for i := 0; i < len(data); {
		c := data[i]
		if c == '\n' {
			line++
			i++
			continue
		}
		if unicode.IsSpace(rune(c)) {
			i++
			continue
		}
		if c == '/' && i+1 < len(data) && data[i+1] == '/' {
			i += 2
			for i < len(data) && data[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(data) && data[i+1] == '*' {
			start := line
			i += 2
			closed := false
			for i < len(data) {
				if data[i] == '\n' {
					line++
				}
				if data[i] == '*' && i+1 < len(data) && data[i+1] == '/' {
					i += 2
					closed = true
					break
				}
				i++
			}
			if !closed {
				return nil, fmt.Errorf("line %d: unterminated block comment", start)
			}
			continue
		}
		if c == '\'' || c == '"' {
			startLine := i
			i++
			var value strings.Builder
			for i < len(data) && data[i] != c {
				if data[i] == '\n' {
					return nil, fmt.Errorf("line %d: unterminated string", startLine)
				}
				if data[i] == '\\' {
					if i+1 >= len(data) {
						return nil, fmt.Errorf("line %d: unterminated string", startLine)
					}
					i++
					switch data[i] {
					case 'n':
						value.WriteByte('\n')
					case 'r':
						value.WriteByte('\r')
					case 't':
						value.WriteByte('\t')
					default:
						value.WriteByte(data[i])
					}
					i++
					continue
				}
				value.WriteByte(data[i])
				i++
			}
			if i >= len(data) {
				return nil, fmt.Errorf("line %d: unterminated string", startLine)
			}
			i++
			tokens = append(tokens, tsToken{Kind: tsString, Text: value.String(), Line: line})
			continue
		}
		if c == '`' {
			startLine := line
			i++
			closed := false
			for i < len(data) {
				if data[i] == '\n' {
					line++
				}
				if data[i] == '\\' {
					i += 2
					continue
				}
				if data[i] == '`' {
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return nil, fmt.Errorf("line %d: unterminated template literal", startLine)
			}
			continue
		}
		if isJSIdentifierStart(c) {
			start := i
			for i < len(data) && isJSIdentifierPart(data[i]) {
				i++
			}
			tokens = append(tokens, tsToken{Kind: tsIdentifier, Text: string(data[start:i]), Line: line})
			continue
		}
		tokens = append(tokens, tsToken{Kind: tsPunct, Text: string(c), Line: line})
		i++
	}
	return tokens, nil
}

func isJSIdentifierStart(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isJSIdentifierPart(c byte) bool {
	return isJSIdentifierStart(c) || c >= '0' && c <= '9'
}

type typeScriptResolutionKind uint8

const (
	typeScriptResolutionExternal typeScriptResolutionKind = iota
	typeScriptResolutionFile
	typeScriptResolutionMissing
)

type typeScriptResolution struct {
	kind   typeScriptResolutionKind
	target string
}

type typeScriptResolver struct {
	root       string
	configs    map[string]typeScriptConfigResult
	incomplete bool
}

type typeScriptConfigResult struct {
	config *typeScriptConfig
	err    error
}

type typeScriptConfig struct {
	BaseURL string
	Paths   map[string][]string
}

func newTypeScriptResolver(root string) *typeScriptResolver {
	return &typeScriptResolver{root: root, configs: map[string]typeScriptConfigResult{}}
}

func (resolver *typeScriptResolver) resolve(file sourceFile, specifier string) (typeScriptResolution, error) {
	if strings.HasPrefix(specifier, ".") {
		if strings.Contains(specifier, "\\") {
			return typeScriptResolution{}, fmt.Errorf("TypeScript import must use forward slashes: %s", specifier)
		}
		matches, err := existingSourceFiles(resolver.root, typeScriptFileCandidates(filepath.Dir(file.Path), specifier))
		if err != nil {
			return typeScriptResolution{}, err
		}
		return oneTypeScriptMatch(matches)
	}
	if strings.HasPrefix(specifier, "/") || strings.HasPrefix(specifier, "node:") || strings.HasPrefix(specifier, "#") {
		return typeScriptResolution{kind: typeScriptResolutionExternal}, nil
	}
	config, err := resolver.configFor(file.Path)
	if err != nil {
		return typeScriptResolution{}, err
	}
	if config == nil {
		return typeScriptResolution{kind: typeScriptResolutionExternal}, nil
	}
	matched := false
	for _, pattern := range orderedTypeScriptPatterns(config.Paths) {
		replacement, ok := typeScriptPatternReplacement(pattern, specifier)
		if !ok {
			continue
		}
		matched = true
		candidates := []string{}
		for _, value := range config.Paths[pattern] {
			candidate := strings.ReplaceAll(value, "*", replacement)
			candidates = append(candidates, typeScriptFileCandidates(config.BaseURL, candidate)...)
		}
		matches, err := existingSourceFiles(resolver.root, candidates)
		if err != nil {
			return typeScriptResolution{}, err
		}
		if len(matches) > 0 {
			return oneTypeScriptMatch(matches)
		}
	}
	if matched {
		return typeScriptResolution{kind: typeScriptResolutionMissing}, nil
	}
	candidates := typeScriptFileCandidates(config.BaseURL, specifier)
	matches, err := existingSourceFiles(resolver.root, candidates)
	if err != nil {
		return typeScriptResolution{}, err
	}
	if len(matches) > 0 {
		return oneTypeScriptMatch(matches)
	}
	return typeScriptResolution{kind: typeScriptResolutionExternal}, nil
}

func oneTypeScriptMatch(matches []string) (typeScriptResolution, error) {
	switch len(matches) {
	case 0:
		return typeScriptResolution{kind: typeScriptResolutionMissing}, nil
	case 1:
		return typeScriptResolution{kind: typeScriptResolutionFile, target: matches[0]}, nil
	default:
		return typeScriptResolution{}, fmt.Errorf("ambiguous TypeScript import resolves to %s", strings.Join(matches, ", "))
	}
}

func typeScriptFileCandidates(base, specifier string) []string {
	target := filepath.Clean(filepath.Join(base, filepath.FromSlash(specifier)))
	candidates := []string{target}
	ext := strings.ToLower(filepath.Ext(target))
	stem := strings.TrimSuffix(target, filepath.Ext(target))
	if ext == ".js" || ext == ".jsx" || ext == ".mjs" || ext == ".cjs" {
		for _, replacement := range []string{".ts", ".tsx", ".mts", ".cts", ".d.ts"} {
			candidates = append(candidates, stem+replacement)
		}
	}
	for _, extension := range []string{".ts", ".tsx", ".mts", ".cts", ".d.ts"} {
		candidates = append(candidates, target+extension, filepath.Join(target, "index"+extension))
	}
	return candidates
}

func orderedTypeScriptPatterns(paths map[string][]string) []string {
	patterns := make([]string, 0, len(paths))
	for pattern := range paths {
		patterns = append(patterns, pattern)
	}
	sort.SliceStable(patterns, func(i, j int) bool {
		iWild := strings.Contains(patterns[i], "*")
		jWild := strings.Contains(patterns[j], "*")
		if iWild != jWild {
			return !iWild
		}
		iPrefix := strings.Index(patterns[i], "*")
		jPrefix := strings.Index(patterns[j], "*")
		if iPrefix >= 0 && jPrefix >= 0 && iPrefix != jPrefix {
			return iPrefix > jPrefix
		}
		return patterns[i] < patterns[j]
	})
	return patterns
}

func typeScriptPatternReplacement(pattern, specifier string) (string, bool) {
	star := strings.Index(pattern, "*")
	if star < 0 {
		return "", pattern == specifier
	}
	if strings.Count(pattern, "*") != 1 {
		return "", false
	}
	prefix, suffix := pattern[:star], pattern[star+1:]
	if !strings.HasPrefix(specifier, prefix) || !strings.HasSuffix(specifier, suffix) {
		return "", false
	}
	return strings.TrimSuffix(strings.TrimPrefix(specifier, prefix), suffix), true
}

func (resolver *typeScriptResolver) configFor(source string) (*typeScriptConfig, error) {
	dir := filepath.Dir(source)
	for {
		if config, ok := resolver.configs[filepath.Join(dir, "tsconfig.json")]; ok {
			return config.config, config.err
		}
		configPath := filepath.Join(dir, "tsconfig.json")
		rel, err := filepath.Rel(resolver.root, configPath)
		if err != nil {
			return nil, err
		}
		if info, err := regularPath(resolver.root, filepath.ToSlash(rel)); err == nil {
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("tsconfig is not a regular file: %s", configPath)
			}
			result := resolver.loadConfig(configPath)
			resolver.configs[configPath] = result
			return result.config, result.err
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		if dir == resolver.root || filepath.Dir(dir) == dir {
			return nil, nil
		}
		if rel, err := filepath.Rel(resolver.root, filepath.Dir(dir)); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, nil
		}
		dir = filepath.Dir(dir)
	}
}

func (resolver *typeScriptResolver) loadConfig(path string) typeScriptConfigResult {
	data, err := readLanguageSource(path)
	if err != nil {
		return typeScriptConfigResult{err: err}
	}
	normalized, err := normalizeJSONC(data)
	if err != nil {
		return typeScriptConfigResult{err: fmt.Errorf("%s: %w", path, err)}
	}
	var raw struct {
		Extends         json.RawMessage `json:"extends"`
		CompilerOptions struct {
			BaseURL string              `json:"baseUrl"`
			Paths   map[string][]string `json:"paths"`
		} `json:"compilerOptions"`
	}
	if err := json.Unmarshal(normalized, &raw); err != nil {
		return typeScriptConfigResult{err: fmt.Errorf("parse %s: %w", path, err)}
	}
	incomplete := len(bytes.TrimSpace(raw.Extends)) > 0 && string(bytes.TrimSpace(raw.Extends)) != "null" && string(bytes.TrimSpace(raw.Extends)) != `""` && string(bytes.TrimSpace(raw.Extends)) != "[]"
	dir := filepath.Dir(path)
	baseURL := dir
	if raw.CompilerOptions.BaseURL != "" {
		baseURL = filepath.Clean(filepath.Join(dir, filepath.FromSlash(raw.CompilerOptions.BaseURL)))
	}
	for pattern := range raw.CompilerOptions.Paths {
		if strings.Count(pattern, "*") > 1 {
			return typeScriptConfigResult{err: fmt.Errorf("%s: tsconfig paths pattern %q has more than one wildcard", path, pattern)}
		}
	}
	if incomplete {
		resolver.incomplete = true
	}
	return typeScriptConfigResult{config: &typeScriptConfig{BaseURL: baseURL, Paths: raw.CompilerOptions.Paths}}
}

func normalizeJSONC(data []byte) ([]byte, error) {
	var out strings.Builder
	inString, escaped := false, false
	lineComment, blockComment := false, false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if lineComment {
			if c == '\n' {
				lineComment = false
				out.WriteByte(c)
			} else {
				out.WriteByte(' ')
			}
			continue
		}
		if blockComment {
			if c == '*' && i+1 < len(data) && data[i+1] == '/' {
				out.WriteString("  ")
				i++
				blockComment = false
			} else if c == '\n' {
				out.WriteByte('\n')
			} else {
				out.WriteByte(' ')
			}
			continue
		}
		if inString {
			out.WriteByte(c)
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
			out.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(data) && data[i+1] == '/' {
			lineComment = true
			out.WriteString("  ")
			i++
			continue
		}
		if c == '/' && i+1 < len(data) && data[i+1] == '*' {
			blockComment = true
			out.WriteString("  ")
			i++
			continue
		}
		out.WriteByte(c)
	}
	if blockComment {
		return nil, fmt.Errorf("unterminated block comment")
	}
	withoutComments := []byte(out.String())
	out.Reset()
	inString, escaped = false, false
	for i := 0; i < len(withoutComments); i++ {
		c := withoutComments[i]
		if inString {
			out.WriteByte(c)
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
			out.WriteByte(c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(withoutComments) && unicode.IsSpace(rune(withoutComments[j])) {
				j++
			}
			if j < len(withoutComments) && (withoutComments[j] == '}' || withoutComments[j] == ']') {
				out.WriteByte(' ')
				continue
			}
		}
		out.WriteByte(c)
	}
	return []byte(out.String()), nil
}
