package deepreview

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/hollis-labs/mentat/internal/audits"
	"github.com/hollis-labs/mentat/internal/domain"
)

var (
	metadataPattern = regexp.MustCompile(`^\*\*([^*]+):\*\*\s*(.+?)\s*$`)
	linkPattern     = regexp.MustCompile(`\[(.+?)\]\((.+?)\)`)
	codeRefPattern  = regexp.MustCompile("`([A-Za-z0-9._/-]+\\.[A-Za-z0-9]+)(?::L(\\d+)(?:-L(\\d+))?)?`")
)

func ParseDir(path, repoID string, provenance audits.Provenance) (*audits.ImportBundle, error) {
	indexPath := filepath.Join(path, "index.md")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("read index: %w", err)
	}
	indexText := string(data)
	meta := parseMetadata(indexText)
	startedAt := parseDate(meta["date"])
	scope := stripTicks(meta["scope"])
	if scope == "" {
		scope = filepath.Base(path)
	}
	auditType := stripTicks(meta["skill"])
	if auditType == "" {
		auditType = "deep-review"
	}
	summary := extractSummary(indexText)
	themeMap := parseThemeAssignments(indexText)
	findingFiles := parseFindingFiles(indexText)
	if len(findingFiles) == 0 {
		glob, _ := filepath.Glob(filepath.Join(path, "*.md"))
		for _, file := range glob {
			if strings.EqualFold(filepath.Base(file), "index.md") {
				continue
			}
			findingFiles = append(findingFiles, filepath.Base(file))
		}
		sort.Strings(findingFiles)
	}

	audit := &audits.Audit{
		RepoID:          repoID,
		Scope:           scope,
		AuditType:       "deep-review",
		Auditor:         stripTicks(meta["reviewer agent"]),
		SummaryMarkdown: summary,
		Status:          "completed",
		Provenance:      provenance,
		StartedAt:       startedAt,
		FinishedAt:      timePtr(startedAt),
	}
	if auditType != "" {
		audit.AuditType = auditType
	}

	findings := make([]audits.Finding, 0, len(findingFiles))
	for _, file := range findingFiles {
		finding, err := parseFindingFile(filepath.Join(path, file), repoID, audit, themeMap[file], provenance)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", file, err)
		}
		findings = append(findings, *finding)
	}

	return &audits.ImportBundle{Audit: audit, Findings: findings}, nil
}

func parseMetadata(body string) map[string]string {
	meta := map[string]string{}
	for _, line := range strings.Split(body, "\n") {
		matches := metadataPattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) != 3 {
			continue
		}
		meta[strings.ToLower(strings.TrimSpace(matches[1]))] = strings.TrimSpace(matches[2])
	}
	return meta
}

func extractSummary(body string) string {
	sections := strings.Split(body, "\n## Findings")
	if len(sections) == 0 {
		return strings.TrimSpace(body)
	}
	lines := strings.Split(strings.TrimSpace(sections[0]), "\n")
	if len(lines) <= 4 {
		return strings.TrimSpace(sections[0])
	}
	return strings.TrimSpace(strings.Join(lines[4:], "\n"))
}

func parseFindingFiles(indexText string) []string {
	seen := map[string]bool{}
	var files []string
	for _, line := range strings.Split(indexText, "\n") {
		match := linkPattern.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}
		target := strings.TrimSpace(match[2])
		if !strings.HasSuffix(strings.ToLower(target), ".md") || strings.EqualFold(target, "index.md") {
			continue
		}
		if !seen[target] {
			seen[target] = true
			files = append(files, target)
		}
	}
	return files
}

func parseThemeAssignments(indexText string) map[string][]audits.Theme {
	themeMap := map[string][]audits.Theme{}
	inByTopic := false
	currentTheme := ""
	for _, rawLine := range strings.Split(indexText, "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "### ") {
			inByTopic = strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(line, "### ")), "By topic")
			currentTheme = ""
			continue
		}
		if !inByTopic {
			continue
		}
		if strings.HasPrefix(line, "**") && strings.HasSuffix(line, "**") {
			currentTheme = strings.Trim(line, "*")
			continue
		}
		match := linkPattern.FindStringSubmatch(line)
		if currentTheme == "" || len(match) != 3 {
			continue
		}
		target := strings.TrimSpace(match[2])
		themeMap[target] = append(themeMap[target], audits.Theme{Name: currentTheme})
	}
	return themeMap
}

func parseFindingFile(path, repoID string, audit *audits.Audit, themes []audits.Theme, provenance audits.Provenance) (*audits.Finding, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read finding file: %w", err)
	}
	body := string(data)
	lines := strings.Split(body, "\n")
	titleLine := ""
	for _, line := range lines {
		if strings.HasPrefix(line, "# ") {
			titleLine = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			break
		}
	}
	severity := severityFromTitle(titleLine, filepath.Base(path))
	title := cleanFindingTitle(titleLine)
	description := firstProblemParagraph(body)
	category := categoryForFinding(severity, title)
	repoIDCopy := repoID
	finding := &audits.Finding{
		Finding: domain.Finding{
			RepoID:      &repoIDCopy,
			Title:       title,
			Category:    category,
			Severity:    severity,
			Description: description,
			Status:      "open",
		},
		BodyMarkdown: body,
		Provenance:   provenance,
		Themes:       themes,
		CodeRefs:     parseCodeRefs(body, repoID),
	}
	if strings.Contains(strings.ToLower(body), "out of scope") || strings.Contains(strings.ToLower(title), "out of scope") {
		finding.IsOutOfScope = true
	}
	if audit.AuditedAtRef != nil {
		finding.AuditedAtRef = audit.AuditedAtRef
	}
	return finding, nil
}

func parseCodeRefs(body, repoID string) []domain.CodeReference {
	matches := codeRefPattern.FindAllStringSubmatch(body, -1)
	seen := map[string]bool{}
	var refs []domain.CodeReference
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		filePath := strings.TrimSpace(match[1])
		if !strings.Contains(filePath, "/") || strings.HasPrefix(filePath, "http") {
			continue
		}
		key := strings.Join(match[1:], "|")
		if seen[key] {
			continue
		}
		seen[key] = true
		ref := domain.CodeReference{RepoID: repoID, FilePath: filePath, RefType: "audit"}
		if match[2] != "" {
			lineStart := atoi(match[2])
			ref.LineStart = &lineStart
		}
		if match[3] != "" {
			lineEnd := atoi(match[3])
			ref.LineEnd = &lineEnd
		}
		refs = append(refs, ref)
	}
	return refs
}

func firstProblemParagraph(body string) string {
	parts := strings.Split(body, "\n## Problem")
	if len(parts) < 2 {
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "**Scope:**") {
				return line
			}
		}
		return ""
	}
	section := strings.TrimSpace(parts[1])
	lines := strings.Split(section, "\n")
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			break
		}
		if trimmed == "" {
			if len(out) > 0 {
				break
			}
			continue
		}
		out = append(out, trimmed)
	}
	return strings.Join(out, " ")
}

func cleanFindingTitle(title string) string {
	title = strings.TrimSpace(title)
	title = regexp.MustCompile(`^\[[^\]]+\]\s*`).ReplaceAllString(title, "")
	return strings.TrimSpace(title)
}

func severityFromTitle(title, filename string) string {
	lower := strings.ToLower(title + " " + filename)
	switch {
	case strings.Contains(lower, "critical"):
		return "critical"
	case strings.Contains(lower, "high"):
		return "high"
	case strings.Contains(lower, "medium"):
		return "medium"
	case strings.Contains(lower, "low"):
		return "low"
	default:
		return "info"
	}
}

func categoryForFinding(severity, title string) string {
	lowerTitle := strings.ToLower(title)
	if strings.Contains(lowerTitle, "praise") || strings.Contains(lowerTitle, "positive") {
		return "strength"
	}
	if severity == "info" {
		return "opportunity"
	}
	return "risk"
}

func parseDate(raw string) time.Time {
	raw = stripTicks(raw)
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC()
		}
	}
	return time.Now().UTC()
}

func stripTicks(value string) string {
	value = strings.ReplaceAll(value, "`", "")
	return strings.TrimSpace(value)
}

func atoi(value string) int {
	var n int
	fmt.Sscanf(value, "%d", &n)
	return n
}

func timePtr(t time.Time) *time.Time {
	return &t
}
