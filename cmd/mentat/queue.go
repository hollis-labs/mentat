package main

import (
	"errors"
	"time"

	libqueue "github.com/hollis-labs/libs/core/util/queue"
	qsqlite "github.com/hollis-labs/libs/core/util/queue/driver/sqlite"

	"github.com/hollis-labs/substrate/module"
	"github.com/hollis-labs/substrate/modules/database"
)

// The queue module's default SQLite driver would use a table named "jobs", which is Mentat's job history, and
// reclaims a job after 60 seconds, which is shorter than a scan. Mentat therefore supplies the driver
// (queue.UseDriverFunc, built when the module starts, over the open database) with its own table names and a
// reservation long enough for its longest job. The module's queue.table and queue.failed_table keys would set the
// names, but there is no key for the reservation yet, so the driver stays here.
const (
	queueTable       = "queue_jobs"
	queueFailedTable = "queue_failed_jobs"
	// queueReserve is how long a popped job stays reserved before it could be handed out again.
	queueReserve = 6 * time.Hour
)

func newQueue(app *module.App) (libqueue.Queue, error) {
	st := database.From(app)
	if st == nil {
		return nil, errors.New("the queue needs the database module")
	}
	return qsqlite.New(st.DB(), qsqlite.Opts{Table: queueTable, FailedTable: queueFailedTable, RetryAfter: queueReserve})
}
