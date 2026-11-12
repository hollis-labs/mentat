package main

import (
	"context"
	"errors"
	"sync"
	"time"

	libqueue "github.com/hollis-labs/libs/core/util/queue"
	qsqlite "github.com/hollis-labs/libs/core/util/queue/driver/sqlite"

	"github.com/hollis-labs/substrate/module"
	"github.com/hollis-labs/substrate/modules/database"
)

// The queue module's own SQLite driver would use a table named "jobs", which is Mentat's job history, and
// reclaims a job after 60 seconds, which is shorter than a scan. Mentat therefore supplies the driver (queue.UseDriver)
// over its own table names and a reservation long enough for its longest job. The database is not open when
// UseDriver must be called, so the driver is built on first use.
const (
	queueTable       = "queue_jobs"
	queueFailedTable = "queue_failed_jobs"
	// queueReserve is how long a popped job stays reserved before it could be handed out again.
	queueReserve = 6 * time.Hour
)

type lazyQueue struct {
	once sync.Once
	app  func() *module.App
	q    libqueue.Queue
	err  error
}

func newLazyQueue(app func() *module.App) *lazyQueue { return &lazyQueue{app: app} }

func (l *lazyQueue) get() (libqueue.Queue, error) {
	l.once.Do(func() {
		a := l.app()
		if a == nil {
			l.err = errors.New("the queue needs the database module")
			return
		}
		st := database.From(a)
		if st == nil {
			l.err = errors.New("the queue needs the database module")
			return
		}
		l.q, l.err = qsqlite.New(st.DB(), qsqlite.Opts{Table: queueTable, FailedTable: queueFailedTable, RetryAfter: queueReserve})
	})
	return l.q, l.err
}

func (l *lazyQueue) Push(ctx context.Context, jobType string, payload []byte, opts ...libqueue.PushOption) error {
	q, err := l.get()
	if err != nil {
		return err
	}
	return q.Push(ctx, jobType, payload, opts...)
}

func (l *lazyQueue) Pop(ctx context.Context, name string) (*libqueue.QueuedJob, error) {
	q, err := l.get()
	if err != nil {
		return nil, err
	}
	return q.Pop(ctx, name)
}

func (l *lazyQueue) Delete(ctx context.Context, id string) error {
	q, err := l.get()
	if err != nil {
		return err
	}
	return q.Delete(ctx, id)
}

func (l *lazyQueue) Release(ctx context.Context, id string, delay time.Duration) error {
	q, err := l.get()
	if err != nil {
		return err
	}
	return q.Release(ctx, id, delay)
}

func (l *lazyQueue) Size(ctx context.Context, name string) (int, error) {
	q, err := l.get()
	if err != nil {
		return 0, err
	}
	return q.Size(ctx, name)
}

func (l *lazyQueue) Failed(ctx context.Context, job *libqueue.QueuedJob, msg string) error {
	q, err := l.get()
	if err != nil {
		return err
	}
	return q.Failed(ctx, job, msg)
}
