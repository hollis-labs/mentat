package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/hollis-labs/mentat/internal/symbols/model"
)

func (s *Store) UpsertSymbol(sym *model.Symbol) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if sym.ID == 0 {
		sym.CreatedAt, _ = time.Parse(time.RFC3339, now)
	}
	sym.UpdatedAt, _ = time.Parse(time.RFC3339, now)

	if sym.ID != 0 {
		_, err := s.db.Exec(`UPDATE symbols
SET kind=?, name=?, qualified_name=?, file_path=?, line_start=?, line_end=?, content_hash=?, signature_hash=?, parent_symbol_id=?, language=?, visibility=?, docstring=?, stale_since_commit=?, updated_at=?
WHERE id=?`,
			sym.Kind, sym.Name, sym.QualifiedName, sym.FilePath, sym.LineStart, sym.LineEnd, sym.ContentHash, sym.SignatureHash,
			sym.ParentSymbolID, sym.Language, sym.Visibility, sym.Docstring, sym.StaleSinceCommit, now, sym.ID,
		)
		if err != nil {
			return fmt.Errorf("update symbol: %w", err)
		}
		return nil
	}

	result, err := s.db.Exec(`INSERT INTO symbols (
repo_id, kind, name, qualified_name, file_path, line_start, line_end, content_hash, signature_hash, parent_symbol_id, language, visibility, docstring, stale_since_commit, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sym.RepoID, sym.Kind, sym.Name, sym.QualifiedName, sym.FilePath, sym.LineStart, sym.LineEnd, sym.ContentHash,
		sym.SignatureHash, sym.ParentSymbolID, sym.Language, sym.Visibility, sym.Docstring, sym.StaleSinceCommit, now, now,
	)
	if err != nil {
		return fmt.Errorf("insert symbol: %w", err)
	}
	sym.ID, _ = result.LastInsertId()
	return nil
}

func (s *Store) GetSymbol(id int64) (*model.Symbol, error) {
	row := s.db.QueryRow(`SELECT id, repo_id, kind, name, qualified_name, file_path, line_start, line_end, content_hash, signature_hash, parent_symbol_id, language, visibility, docstring, stale_since_commit, created_at, updated_at
FROM symbols WHERE id = ?`, id)
	sym, err := scanSymbol(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get symbol: %w", err)
	}
	return sym, nil
}

func (s *Store) FindSymbolByQualifiedName(repoID, qualifiedName string) (*model.Symbol, error) {
	row := s.db.QueryRow(`SELECT id, repo_id, kind, name, qualified_name, file_path, line_start, line_end, content_hash, signature_hash, parent_symbol_id, language, visibility, docstring, stale_since_commit, created_at, updated_at
FROM symbols WHERE repo_id = ? AND qualified_name = ? ORDER BY file_path, line_start, id LIMIT 1`, repoID, qualifiedName)
	sym, err := scanSymbol(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find symbol by qualified name: %w", err)
	}
	return sym, nil
}

func (s *Store) FindSymbolByFileQualifiedName(repoID, filePath, qualifiedName string) (*model.Symbol, error) {
	row := s.db.QueryRow(`SELECT id, repo_id, kind, name, qualified_name, file_path, line_start, line_end, content_hash, signature_hash, parent_symbol_id, language, visibility, docstring, stale_since_commit, created_at, updated_at
FROM symbols WHERE repo_id = ? AND file_path = ? AND qualified_name = ? ORDER BY id LIMIT 1`, repoID, filePath, qualifiedName)
	sym, err := scanSymbol(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find symbol by file and qualified name: %w", err)
	}
	return sym, nil
}

func (s *Store) FindSymbolByContentHash(repoID, contentHash string) (*model.Symbol, error) {
	row := s.db.QueryRow(`SELECT id, repo_id, kind, name, qualified_name, file_path, line_start, line_end, content_hash, signature_hash, parent_symbol_id, language, visibility, docstring, stale_since_commit, created_at, updated_at
FROM symbols WHERE repo_id = ? AND content_hash = ? ORDER BY id LIMIT 1`, repoID, contentHash)
	sym, err := scanSymbol(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find symbol by content hash: %w", err)
	}
	return sym, nil
}

func (s *Store) FindSymbolByFileContentHash(repoID, filePath, contentHash string) (*model.Symbol, error) {
	row := s.db.QueryRow(`SELECT id, repo_id, kind, name, qualified_name, file_path, line_start, line_end, content_hash, signature_hash, parent_symbol_id, language, visibility, docstring, stale_since_commit, created_at, updated_at
FROM symbols WHERE repo_id = ? AND file_path = ? AND content_hash = ? ORDER BY id LIMIT 1`, repoID, filePath, contentHash)
	sym, err := scanSymbol(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find symbol by file and content hash: %w", err)
	}
	return sym, nil
}

func (s *Store) SearchSymbols(filter model.SearchFilter) ([]model.Symbol, error) {
	query := `SELECT id, repo_id, kind, name, qualified_name, file_path, line_start, line_end, content_hash, signature_hash, parent_symbol_id, language, visibility, docstring, stale_since_commit, created_at, updated_at FROM symbols WHERE 1=1`
	var args []any
	if filter.RepoID != "" {
		query += " AND repo_id = ?"
		args = append(args, filter.RepoID)
	}
	if filter.Kind != "" {
		query += " AND kind = ?"
		args = append(args, filter.Kind)
	}
	if filter.Language != "" {
		query += " AND language = ?"
		args = append(args, filter.Language)
	}
	if filter.Query != "" {
		query += " AND (name LIKE ? OR qualified_name LIKE ? OR file_path LIKE ?)"
		like := "%" + filter.Query + "%"
		args = append(args, like, like, like)
	}
	query += " ORDER BY "
	if filter.Query != "" {
		query += "CASE WHEN name = ? THEN 0 WHEN qualified_name = ? THEN 1 WHEN name LIKE ? THEN 2 ELSE 3 END, "
		args = append(args, filter.Query, filter.Query, filter.Query+"%")
	}
	query += "CASE WHEN file_path LIKE 'testdata/%' OR file_path LIKE '%/testdata/%' THEN 1 ELSE 0 END, qualified_name"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("search symbols: %w", err)
	}
	defer rows.Close()

	var out []model.Symbol
	for rows.Next() {
		sym, err := scanSymbol(rows)
		if err != nil {
			return nil, fmt.Errorf("scan symbol: %w", err)
		}
		out = append(out, *sym)
	}
	return out, rows.Err()
}

func (s *Store) ListSymbolsByFile(repoID, filePath string) ([]model.Symbol, error) {
	rows, err := s.db.Query(`SELECT id, repo_id, kind, name, qualified_name, file_path, line_start, line_end, content_hash, signature_hash, parent_symbol_id, language, visibility, docstring, stale_since_commit, created_at, updated_at
FROM symbols WHERE repo_id = ? AND file_path = ? ORDER BY qualified_name`, repoID, filePath)
	if err != nil {
		return nil, fmt.Errorf("list symbols by file: %w", err)
	}
	defer rows.Close()

	var out []model.Symbol
	for rows.Next() {
		sym, err := scanSymbol(rows)
		if err != nil {
			return nil, fmt.Errorf("scan symbol by file: %w", err)
		}
		out = append(out, *sym)
	}
	return out, rows.Err()
}

func (s *Store) SymbolStats(filter model.StatsFilter) (*model.Stats, error) {
	stats := &model.Stats{
		RepoID:         filter.RepoID,
		ByKind:         map[string]int{},
		ByLanguage:     map[string]int{},
		ByLanguageKind: map[string]map[string]int{},
	}

	where := ""
	var args []any
	if filter.RepoID != "" {
		where = " WHERE repo_id = ?"
		args = append(args, filter.RepoID)
	}

	if err := s.db.QueryRow("SELECT COUNT(*), COALESCE(SUM(CASE WHEN stale_since_commit IS NOT NULL AND stale_since_commit != '' THEN 1 ELSE 0 END), 0) FROM symbols"+where, args...).Scan(&stats.Total, &stats.Stale); err != nil {
		return nil, fmt.Errorf("symbol stats totals: %w", err)
	}

	rows, err := s.db.Query("SELECT language, kind, COUNT(*) FROM symbols"+where+" GROUP BY language, kind ORDER BY language, kind", args...)
	if err != nil {
		return nil, fmt.Errorf("symbol stats breakdown: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var language, kind string
		var count int
		if err := rows.Scan(&language, &kind, &count); err != nil {
			return nil, fmt.Errorf("scan symbol stats: %w", err)
		}
		stats.ByLanguage[language] += count
		stats.ByKind[kind] += count
		if stats.ByLanguageKind[language] == nil {
			stats.ByLanguageKind[language] = map[string]int{}
		}
		stats.ByLanguageKind[language][kind] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return stats, nil
}

type symbolScanner interface {
	Scan(dest ...any) error
}

func scanSymbol(scanner symbolScanner) (*model.Symbol, error) {
	var sym model.Symbol
	var createdAt, updatedAt string
	var visibility sql.NullString
	var docstring string
	var staleSinceCommit sql.NullString
	var parentSymbolID sql.NullInt64
	if err := scanner.Scan(
		&sym.ID, &sym.RepoID, &sym.Kind, &sym.Name, &sym.QualifiedName, &sym.FilePath,
		&sym.LineStart, &sym.LineEnd, &sym.ContentHash, &sym.SignatureHash, &parentSymbolID,
		&sym.Language, &visibility, &docstring, &staleSinceCommit, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	if visibility.Valid {
		sym.Visibility = visibility.String
	}
	sym.Docstring = docstring
	if staleSinceCommit.Valid {
		sym.StaleSinceCommit = &staleSinceCommit.String
	}
	if parentSymbolID.Valid {
		id := parentSymbolID.Int64
		sym.ParentSymbolID = &id
	}
	sym.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	sym.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &sym, nil
}

func normalizeVisibility(v string) string {
	return strings.TrimSpace(v)
}
