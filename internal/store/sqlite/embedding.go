package sqlite

import (
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/hollis-labs/mentat/internal/embed"
)

type EmbeddingRow struct {
	TargetKind  string
	TargetID    int64
	Model       string
	Dim         int
	Vector      []float32
	ContentHash string
	CreatedAt   time.Time
}

type SymbolEmbeddingTarget struct {
	ID          int64
	RepoID      string
	ContentHash string
	Text        string
}

type FindingEmbeddingTarget struct {
	ID          int64
	RepoID      string
	ContentHash string
	Text        string
}

func (s *Store) UpsertEmbedding(row EmbeddingRow) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`INSERT INTO embeddings (target_kind, target_id, model, dim, vector, content_hash, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(target_kind, target_id, model) DO UPDATE SET
dim=excluded.dim,
vector=excluded.vector,
content_hash=excluded.content_hash,
created_at=excluded.created_at`,
		row.TargetKind, row.TargetID, row.Model, row.Dim, float32ToBlob(row.Vector), row.ContentHash, now,
	)
	if err != nil {
		return fmt.Errorf("upsert embedding: %w", err)
	}
	return nil
}

func (s *Store) GetEmbedding(targetKind string, targetID int64, model string) (*EmbeddingRow, error) {
	row := s.db.QueryRow(`SELECT target_kind, target_id, model, dim, vector, content_hash, created_at
FROM embeddings WHERE target_kind = ? AND target_id = ? AND model = ?`, targetKind, targetID, model)
	var out EmbeddingRow
	var vector []byte
	var createdAt string
	if err := row.Scan(&out.TargetKind, &out.TargetID, &out.Model, &out.Dim, &vector, &out.ContentHash, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get embedding: %w", err)
	}
	out.Vector = blobToFloat32(vector)
	out.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &out, nil
}

func (s *Store) DeleteEmbeddingsForRepo(repoID string) error {
	_, err := s.db.Exec(`DELETE FROM embeddings
WHERE (target_kind = 'symbol' AND target_id IN (SELECT id FROM symbols WHERE repo_id = ?))
   OR (target_kind = 'finding' AND target_id IN (SELECT id FROM findings WHERE repo_id = ?))`, repoID, repoID)
	if err != nil {
		return fmt.Errorf("delete repo embeddings: %w", err)
	}
	return nil
}

func (s *Store) ListSymbolEmbeddingTargets(repoID string) ([]SymbolEmbeddingTarget, error) {
	rows, err := s.db.Query(`SELECT id, repo_id, content_hash, qualified_name || char(10) || file_path || char(10) || docstring
FROM symbols WHERE repo_id = ? ORDER BY id`, repoID)
	if err != nil {
		return nil, fmt.Errorf("list symbol embedding targets: %w", err)
	}
	defer rows.Close()

	var out []SymbolEmbeddingTarget
	for rows.Next() {
		var item SymbolEmbeddingTarget
		if err := rows.Scan(&item.ID, &item.RepoID, &item.ContentHash, &item.Text); err != nil {
			return nil, fmt.Errorf("scan symbol embedding target: %w", err)
		}
		item.ContentHash = embed.HashText(item.Text)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListFindingEmbeddingTargets(repoID string) ([]FindingEmbeddingTarget, error) {
	rows, err := s.db.Query(`SELECT id, COALESCE(repo_id, ?), '', title || char(10) || description || char(10) || body_markdown
FROM findings WHERE repo_id = ? ORDER BY id`, repoID, repoID)
	if err != nil {
		return nil, fmt.Errorf("list finding embedding targets: %w", err)
	}
	defer rows.Close()

	var out []FindingEmbeddingTarget
	for rows.Next() {
		var item FindingEmbeddingTarget
		if err := rows.Scan(&item.ID, &item.RepoID, &item.ContentHash, &item.Text); err != nil {
			return nil, fmt.Errorf("scan finding embedding target: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func float32ToBlob(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		bits := math.Float32bits(f)
		buf[i*4] = byte(bits)
		buf[i*4+1] = byte(bits >> 8)
		buf[i*4+2] = byte(bits >> 16)
		buf[i*4+3] = byte(bits >> 24)
	}
	return buf
}

func blobToFloat32(b []byte) []float32 {
	if len(b) == 0 {
		return nil
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		bits := uint32(b[i*4]) | uint32(b[i*4+1])<<8 | uint32(b[i*4+2])<<16 | uint32(b[i*4+3])<<24
		out[i] = math.Float32frombits(bits)
	}
	return out
}
