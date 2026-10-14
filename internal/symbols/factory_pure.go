//go:build !treesitter

package symbols

import scipingester "github.com/hollis-labs/mentat/internal/symbols/scip"

func NewIngester(cfg Config) Ingester {
	return scipingester.NewIngester(cfg.Store)
}
