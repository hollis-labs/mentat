//go:build treesitter

package symbols

import (
	"context"

	"github.com/hollis-labs/mentat/internal/symbols/model"
	scipingester "github.com/hollis-labs/mentat/internal/symbols/scip"
	treesitteringester "github.com/hollis-labs/mentat/internal/symbols/treesitter"
)

type compositeIngester struct {
	primary model.Ingester
	augment model.Ingester
}

func (c compositeIngester) Ingest(ctx context.Context, req model.IngestRequest) (model.IngestResult, error) {
	primary, err := c.primary.Ingest(ctx, req)
	if err != nil {
		return primary, err
	}
	augmentReq := req
	augmentReq.CommitRef = ""
	augment, err := c.augment.Ingest(ctx, augmentReq)
	if err != nil {
		return primary, err
	}
	primary.Inserted += augment.Inserted
	primary.Updated += augment.Updated
	primary.Drifted += augment.Drifted
	return primary, nil
}

func NewIngester(cfg Config) Ingester {
	return compositeIngester{
		primary: treesitteringester.NewIngester(cfg.Store),
		augment: scipingester.NewAugmentingIngester(cfg.Store),
	}
}
