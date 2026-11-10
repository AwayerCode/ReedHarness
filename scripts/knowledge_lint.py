#!/usr/bin/env python3
import argparse
import re
import subprocess
import sys
from pathlib import Path


def discover_root():
    result = subprocess.run(
        ["git", "rev-parse", "--show-toplevel"],
        cwd=Path.cwd(),
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if result.returncode != 0:
        print("Run knowledge_lint.py from within the target Git workspace.", file=sys.stderr)
        sys.exit(result.returncode)
    root = Path(result.stdout.strip()).resolve()
    if not (root / "AGENTS.md").is_file():
        print(f"Workspace marker not found: {root / 'AGENTS.md'}", file=sys.stderr)
        sys.exit(2)
    return root


ROOT = discover_root()

SKIP_PARTS = {
    ".git",
    ".agents",
    ".codex",
    "node_modules",
}

FILLER_PATTERNS = [
    "本文将",
    "下面",
    "如下",
    "总结一下",
    "换句话说",
    "也就是说",
    "可以说是",
    "后续再展开",
]

NAV_PATTERNS = [
    "入口见",
    "相关文档",
    "关联文档",
    "知识地图",
    "目录结构",
    "详见",
]

SIMILAR_HEADINGS = [
    "核心判断",
    "核心认知",
    "核心结论",
    "核心观点",
]


class Issue:
    def __init__(self, level, path, line, message):
        self.level = level
        self.path = path
        self.line = line
        self.message = message

    def __str__(self):
        loc = f"{self.path}:{self.line}" if self.line else str(self.path)
        return f"{self.level.upper()}: {loc}: {self.message}"


def rel(path):
    try:
        return path.resolve().relative_to(ROOT)
    except ValueError:
        return path


def run_git(args, null_terminated=False):
    result = subprocess.run(
        ["git", *args],
        cwd=ROOT,
        text=not null_terminated,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if result.returncode != 0:
        stderr = result.stderr
        if isinstance(stderr, bytes):
            stderr = stderr.decode("utf-8", errors="replace")
        print(stderr.strip(), file=sys.stderr)
        sys.exit(result.returncode)
    if null_terminated:
        return [
            name.decode("utf-8", errors="surrogateescape")
            for name in result.stdout.split(b"\0")
            if name
        ]
    return result.stdout.splitlines()


def is_skipped(path):
    relative = rel(path)
    return any(part in SKIP_PARTS for part in relative.parts)


def all_markdown_files():
    return [
        p
        for p in ROOT.rglob("*.md")
        if p.is_file() and not is_skipped(p)
    ]


def changed_files(staged=False):
    args = ["diff", "--name-only", "--diff-filter=ACMR", "-z"]
    if staged:
        args.insert(1, "--cached")
    names = run_git(args, null_terminated=True)
    if not staged:
        names.extend(
            run_git(
                ["ls-files", "--others", "--exclude-standard", "-z"],
                null_terminated=True,
            )
        )
    paths = []
    for name in names:
        path = ROOT / name
        if path.suffix == ".md" and path.exists() and not is_skipped(path):
            paths.append(path)
    return paths


def explicit_files(values):
    paths = []
    for value in values:
        path = Path(value)
        if not path.is_absolute():
            path = ROOT / path
        if path.is_dir():
            paths.extend(
                p for p in path.rglob("*.md") if p.is_file() and not is_skipped(p)
            )
        elif path.suffix == ".md" and path.exists() and not is_skipped(path):
            paths.append(path)
    return paths


def is_overview(path):
    name = path.name
    stem = path.stem
    return name.startswith("0-") or "总览" in stem or stem in {"AGENTS", "NOTE"}


def decode_file(path):
    raw = path.read_bytes()
    issues = []
    if b"\r\n" in raw:
        issues.append(Issue("error", rel(path), 0, "uses CRLF line endings"))
    try:
        text = raw.decode("utf-8")
    except UnicodeDecodeError:
        return "", [Issue("error", rel(path), 0, "is not valid UTF-8")]
    if text and not text.endswith("\n"):
        issues.append(Issue("error", rel(path), 0, "missing final newline"))
    return text, issues


def check_headings(path, lines):
    issues = []
    h1_lines = []
    previous_level = 0
    fence = None
    for index, line in enumerate(lines, 1):
        marker = re.match(r"^\s{0,3}(`{3,}|~{3,})", line)
        if marker:
            token = marker.group(1)
            if fence is None:
                fence = token
            elif token[0] == fence[0] and len(token) >= len(fence):
                fence = None
            continue
        if fence is not None:
            continue
        match = re.match(r"^(#{1,6})\s+\S", line)
        if not match:
            continue
        level = len(match.group(1))
        if level == 1:
            h1_lines.append(index)
        if previous_level and level > previous_level + 1:
            issues.append(
                Issue("error", rel(path), index, "heading level jumps too far")
            )
        previous_level = level
    if not h1_lines:
        issues.append(Issue("error", rel(path), 0, "missing first-level heading"))
    elif len(h1_lines) > 1:
        issues.append(Issue("error", rel(path), h1_lines[1], "multiple first-level headings"))
    return issues


def check_code_fences(path, lines):
    issues = []
    in_fence = False
    fence_start = 0
    fence_lines = []
    for index, line in enumerate(lines, 1):
        if line.startswith("```"):
            if not in_fence:
                in_fence = True
                fence_start = index
                fence_lines = []
                if line.strip() == "```text" and "skills" not in rel(path).parts:
                    issues.append(
                        Issue(
                            "error",
                            rel(path),
                            index,
                            "text code fence is not allowed; use prose, lists, inline code, or a specific language",
                        )
                    )
            else:
                if is_overview(path) and looks_like_prose_model(fence_lines):
                    issues.append(
                        Issue(
                            "error",
                            rel(path),
                            fence_start,
                            "overview uses code block for a prose model or judgment",
                        )
                    )
                in_fence = False
        elif in_fence:
            fence_lines.append(line)
    if in_fence:
        issues.append(Issue("error", rel(path), fence_start, "unclosed code block"))
    return issues


def looks_like_prose_model(lines):
    content = "\n".join(line.strip() for line in lines if line.strip())
    if not content:
        return False
    if any(token in content for token in ["=", "→", "↓", "："]):
        code_markers = ["{", "}", ";", "def ", "class ", "const ", "let ", "function ", "#!/"]
        return not any(marker in content for marker in code_markers)
    return False


def check_content_smells(path, lines):
    issues = []
    text = "\n".join(lines)
    for pattern in FILLER_PATTERNS:
        for index, line in enumerate(lines, 1):
            if pattern in line:
                issues.append(Issue("warning", rel(path), index, f"filler phrase `{pattern}`"))
    if is_overview(path):
        if len(lines) > 100:
            issues.append(Issue("warning", rel(path), 0, "overview is longer than 100 lines"))
        for pattern in NAV_PATTERNS:
            for index, line in enumerate(lines, 1):
                if pattern in line:
                    issues.append(Issue("warning", rel(path), index, f"overview navigation signal `{pattern}`"))
        if re.search(r"^##\s+(目录|关系|关联|相关)", text, re.M):
            issues.append(Issue("warning", rel(path), 0, "overview has directory or relation section"))

    heading_hits = []
    for index, line in enumerate(lines, 1):
        for heading in SIMILAR_HEADINGS:
            if re.match(rf"^##+\s+{re.escape(heading)}\b", line):
                heading_hits.append((heading, index))
    if len(heading_hits) >= 2:
        issues.append(
            Issue(
                "warning",
                rel(path),
                heading_hits[1][1],
                "similar core-heading sections coexist",
            )
        )

    if has_tail_link_dump(lines):
        issues.append(Issue("warning", rel(path), 0, "tail section concentrates related links"))
    return issues


def has_tail_link_dump(lines):
    tail = lines[-12:]
    hits = 0
    for line in tail:
        if re.search(r"(关联|相关|详见|入口见).*(\[[^\]]+\]\([^)]+\)|`[^`]+\.md`)", line):
            hits += 1
    return hits >= 3


def check_basic_markdown(path, lines):
    issues = []
    for index, line in enumerate(lines, 1):
        if line.rstrip("\n").endswith(" "):
            issues.append(Issue("error", rel(path), index, "trailing whitespace"))
        if line.strip() == "":
            continue
        if re.match(r"^##+\s*$", line):
            issues.append(Issue("warning", rel(path), index, "empty heading"))
    return issues


def check_style_syntax(path, lines):
    issues = []
    numbered_heading = re.compile(
        r"^#{2,6}\s+(?:[（(][一二三四五六七八九十0-9]+[）)]|[一二三四五六七八九十]+、|[0-9]+[.、])\s*"
    )
    bold_heading = re.compile(r"^#{1,6}\s+\*\*.+\*\*\s*$")
    bold_only = re.compile(r"^\*\*(.+)\*\*\s*$")
    ordered_item = re.compile(r"^(\s*)([0-9]+)\.\s+(.*)")
    list_leading_bold_label = re.compile(r"^\s*(?:[-*]|\d+\.)\s+\*\*[^*]+?\*\*[：:]")
    ordered_expected_by_indent = {}

    for index, line in enumerate(lines, 1):
        if numbered_heading.match(line):
            issues.append(Issue("warning", rel(path), index, "heading uses numbering; headings should be semantic only"))
        if bold_heading.match(line):
            issues.append(Issue("warning", rel(path), index, "heading text should not be bold"))
        if bold_only.match(line):
            issues.append(Issue("warning", rel(path), index, "standalone bold text should be a heading, quote, or paragraph"))
        if re.match(r"^\s*\*\s+", line):
            issues.append(Issue("warning", rel(path), index, "unordered list should use `-`"))
        if re.match(r"^\s*[0-9]+\.\s{2,}", line):
            issues.append(Issue("warning", rel(path), index, "ordered-list marker should be followed by one space"))
        ordered_match = ordered_item.match(line)
        if ordered_match:
            indent = len(ordered_match.group(1))
            marker = int(ordered_match.group(2))
            expected = ordered_expected_by_indent.get(indent, 1)
            if marker != expected:
                issues.append(Issue("warning", rel(path), index, "ordered-list marker should be sequential"))
            ordered_expected_by_indent[indent] = marker + 1
            for nested_indent in list(ordered_expected_by_indent):
                if nested_indent > indent:
                    del ordered_expected_by_indent[nested_indent]
            if indent > 0:
                issues.append(Issue("warning", rel(path), index, "nested list should use `-` unless another ordered sequence is required"))
        elif line.strip() == "":
            ordered_expected_by_indent = {}
        else:
            ordered_expected_by_indent = {}
        if list_leading_bold_label.match(line):
            issues.append(Issue("warning", rel(path), index, "list item should not use a leading bold label"))
        if line.strip() == "---":
            issues.append(Issue("warning", rel(path), index, "horizontal divider is not used; use heading structure"))
        heading = re.match(r"^#{1,6}\s+(.+?)\s*$", line)
        if heading and heading.group(1).endswith(("：", ":", "。", ".")):
            issues.append(Issue("warning", rel(path), index, "heading should not end with punctuation"))
    return issues


def check_file(path):
    text, issues = decode_file(path)
    if not text:
        if not any(issue.message == "is not valid UTF-8" for issue in issues):
            issues.extend(check_headings(path, []))
        return issues
    lines = text.splitlines()
    issues.extend(check_headings(path, lines))
    issues.extend(check_code_fences(path, lines))
    issues.extend(check_basic_markdown(path, lines))
    if not {"skills", ".codex"}.intersection(rel(path).parts):
        issues.extend(check_style_syntax(path, lines))
        issues.extend(check_content_smells(path, lines))
    if path.parent == ROOT and path.name not in {"AGENTS.md", "CLAUDE.md", "README.md"}:
        issues.append(Issue("info", rel(path), 0, "Markdown file lives at repository root"))
    if len(lines) > 300:
        issues.append(Issue("info", rel(path), 0, "document is longer than 300 lines"))
    return issues


def main():
    parser = argparse.ArgumentParser(description="Lint Markdown in the target workspace.")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--changed", action="store_true", help="check unstaged changed Markdown files")
    mode.add_argument("--staged", action="store_true", help="check staged Markdown files")
    mode.add_argument("--all", action="store_true", help="check all Markdown files")
    parser.add_argument("paths", nargs="*", help="specific files or directories")
    args = parser.parse_args()

    if args.all:
        paths = all_markdown_files()
    elif args.staged:
        paths = changed_files(staged=True)
    elif args.changed:
        paths = changed_files(staged=False)
    elif args.paths:
        paths = explicit_files(args.paths)
    else:
        parser.error("use --changed, --staged, --all, or pass Markdown paths")

    paths = sorted(set(paths))
    issues = []
    for path in paths:
        issues.extend(check_file(path))
    level_order = {"error": 0, "warning": 1, "info": 2}
    issues.sort(key=lambda item: (level_order[item.level], str(item.path), item.line))
    for issue in issues:
        print(issue)

    errors = sum(1 for issue in issues if issue.level == "error")
    warnings = sum(1 for issue in issues if issue.level == "warning")
    infos = sum(1 for issue in issues if issue.level == "info")
    print(f"\nChecked {len(paths)} file(s): {errors} error(s), {warnings} warning(s), {infos} info(s)")
    return 1 if errors else 0


if __name__ == "__main__":
    sys.exit(main())
