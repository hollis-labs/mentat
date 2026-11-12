package audits

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func ExportBundle(bundle *AuditBundle, outDir string) error {
	if bundle == nil || bundle.Audit == nil {
		return fmt.Errorf("audit bundle is required")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("mkdir export dir: %w", err)
	}
	indexPath := filepath.Join(outDir, "index.md")
	if err := os.WriteFile(indexPath, []byte(renderIndex(bundle)), 0o644); err != nil {
		return fmt.Errorf("write index: %w", err)
	}
	for i, finding := range bundle.Findings {
		filename := fmt.Sprintf("%02d-%s-%s.md", i+1, finding.Severity, slugify(finding.Title))
		if err := os.WriteFile(filepath.Join(outDir, filename), []byte(finding.BodyMarkdown), 0o644); err != nil {
			return fmt.Errorf("write finding file: %w", err)
		}
	}
	return nil
}

func renderIndex(bundle *AuditBundle) string {
	var b strings.Builder
	filenames := map[string]string{}
	for i, finding := range bundle.Findings {
		filenames[finding.Title] = fmt.Sprintf("%02d-%s-%s.md", i+1, finding.Severity, slugify(finding.Title))
	}
	fmt.Fprintf(&b, "# %s — %s\n\n", bundle.Audit.Scope, bundle.Audit.AuditType)
	fmt.Fprintf(&b, "**Scope:** `%s`\n", bundle.Audit.Scope)
	fmt.Fprintf(&b, "**Reviewer agent:** `%s`\n", bundle.Audit.Auditor)
	fmt.Fprintf(&b, "**Skill:** `%s`\n\n", bundle.Audit.AuditType)
	if bundle.Audit.SummaryMarkdown != "" {
		b.WriteString(bundle.Audit.SummaryMarkdown)
		b.WriteString("\n\n")
	}
	b.WriteString("## Findings\n\n")
	b.WriteString("### By severity\n\n")
	grouped := map[string][]Finding{}
	for _, finding := range bundle.Findings {
		grouped[finding.Severity] = append(grouped[finding.Severity], finding)
	}
	for _, severity := range []string{"critical", "high", "medium", "low", "info"} {
		findings := grouped[severity]
		if len(findings) == 0 {
			continue
		}
		fmt.Fprintf(&b, "**%s**\n", strings.Title(severity)) //nolint:staticcheck // SA1019: kept for identical output; the replacement (x/text/cases) would add a dependency
		for _, finding := range findings {
			fmt.Fprintf(&b, "- [%s](%s)\n", finding.Title, filenames[finding.Title])
		}
		b.WriteString("\n")
	}
	b.WriteString("### By topic\n\n")
	themeMap := map[string][]Finding{}
	for _, finding := range bundle.Findings {
		for _, theme := range finding.Themes {
			themeMap[theme.Name] = append(themeMap[theme.Name], finding)
		}
	}
	themeNames := make([]string, 0, len(themeMap))
	for name := range themeMap {
		themeNames = append(themeNames, name)
	}
	sort.Strings(themeNames)
	for _, theme := range themeNames {
		fmt.Fprintf(&b, "**%s**\n", theme)
		for _, finding := range themeMap[theme] {
			fmt.Fprintf(&b, "- [%s](%s)\n", finding.Title, filenames[finding.Title])
		}
		b.WriteString("\n")
	}
	return b.String()
}

func slugify(value string) string {
	value = strings.ToLower(value)
	replacer := strings.NewReplacer(" ", "-", "/", "-", "_", "-", "`", "", "'", "", "\"", "", ":", "", ",", "", ".", "", "(", "", ")", "", "—", "-")
	value = replacer.Replace(value)
	value = strings.Trim(value, "-")
	return value
}
