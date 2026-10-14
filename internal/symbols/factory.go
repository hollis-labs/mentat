package symbols

import (
	"context"
	"fmt"

	"github.com/hollis-labs/mentat/internal/symbols/model"
)

type noopIngester struct {
	mode string
}

func (n noopIngester) Ingest(_ context.Context, _ model.IngestRequest) (model.IngestResult, error) {
	return model.IngestResult{}, fmt.Errorf("%s symbol ingester is not implemented yet", n.mode)
}

func newNoopIngester(mode string) model.Ingester {
	return noopIngester{mode: mode}
}
