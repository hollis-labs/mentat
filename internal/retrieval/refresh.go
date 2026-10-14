package retrieval

import (
	"context"
	"fmt"
	"strings"

	"github.com/hollis-labs/mentat/internal/embed"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
)

type RefreshReport struct {
	RepoID           string `json:"repo_id"`
	Profile          string `json:"profile"`
	Model            string `json:"model"`
	SymbolsEmbedded  int    `json:"symbols_embedded"`
	FindingsEmbedded int    `json:"findings_embedded"`
	Skipped          int    `json:"skipped"`
}

func RefreshRepoEmbeddings(ctx context.Context, store *sqlite.Store, repoID string, force bool) (*RefreshReport, error) {
	repo, err := store.GetRepo(repoID)
	if err != nil {
		return nil, err
	}
	if repo == nil {
		return nil, fmt.Errorf("repo not found: %s", repoID)
	}
	if repo.EmbeddingProfile == "" || repo.EmbeddingProfile == "none" {
		return nil, fmt.Errorf("repo %s has embeddings disabled", repoID)
	}

	manager := embed.NewManager()
	cfg, err := manager.Profile(repo.EmbeddingProfile)
	if err != nil {
		return nil, err
	}

	report := &RefreshReport{RepoID: repoID, Profile: repo.EmbeddingProfile, Model: cfg.Model}

	symbols, err := store.ListSymbolEmbeddingTargets(repoID)
	if err != nil {
		return nil, err
	}
	symbolTexts := make([]string, 0, len(symbols))
	symbolItems := make([]sqlite.SymbolEmbeddingTarget, 0, len(symbols))
	for _, item := range symbols {
		if strings.TrimSpace(item.Text) == "" {
			report.Skipped++
			continue
		}
		existing, err := store.GetEmbedding("symbol", item.ID, cfg.Model)
		if err != nil {
			return nil, err
		}
		if !force && existing != nil && existing.ContentHash == item.ContentHash {
			report.Skipped++
			continue
		}
		symbolItems = append(symbolItems, item)
		symbolTexts = append(symbolTexts, item.Text)
	}
	if len(symbolTexts) > 0 {
		vectors, err := manager.EmbedBatch(ctx, cfg.Provider, cfg.Model, symbolTexts)
		if err != nil {
			return nil, err
		}
		for i, vec := range vectors {
			if err := store.UpsertEmbedding(sqlite.EmbeddingRow{
				TargetKind:  "symbol",
				TargetID:    symbolItems[i].ID,
				Model:       cfg.Model,
				Dim:         len(vec),
				Vector:      vec,
				ContentHash: symbolItems[i].ContentHash,
			}); err != nil {
				return nil, err
			}
			report.SymbolsEmbedded++
		}
	}

	findings, err := store.ListFindingEmbeddingTargets(repoID)
	if err != nil {
		return nil, err
	}
	findingTexts := make([]string, 0, len(findings))
	findingItems := make([]sqlite.FindingEmbeddingTarget, 0, len(findings))
	for _, item := range findings {
		if strings.TrimSpace(item.Text) == "" {
			report.Skipped++
			continue
		}
		item.ContentHash = embed.HashText(item.Text)
		existing, err := store.GetEmbedding("finding", item.ID, cfg.Model)
		if err != nil {
			return nil, err
		}
		if !force && existing != nil && existing.ContentHash == item.ContentHash {
			report.Skipped++
			continue
		}
		findingItems = append(findingItems, item)
		findingTexts = append(findingTexts, item.Text)
	}
	if len(findingTexts) > 0 {
		vectors, err := manager.EmbedBatch(ctx, cfg.Provider, cfg.Model, findingTexts)
		if err != nil {
			return nil, err
		}
		for i, vec := range vectors {
			if err := store.UpsertEmbedding(sqlite.EmbeddingRow{
				TargetKind:  "finding",
				TargetID:    findingItems[i].ID,
				Model:       cfg.Model,
				Dim:         len(vec),
				Vector:      vec,
				ContentHash: findingItems[i].ContentHash,
			}); err != nil {
				return nil, err
			}
			report.FindingsEmbedded++
		}
	}

	return report, nil
}
