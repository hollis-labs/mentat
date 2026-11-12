package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	libsched "github.com/hollis-labs/libs/core/util/scheduler"
	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/schedmap"
)

// ScheduleStore is the part of the scheduler module's store (libs sqlstore) the service uses.
type ScheduleStore interface {
	CreateSchedule(ctx context.Context, sch libsched.Schedule) error
	GetSchedule(ctx context.Context, id string) (libsched.Schedule, bool, error)
	ListSchedules(ctx context.Context) ([]libsched.Schedule, error)
	DeleteSchedule(ctx context.Context, id string) error
}

func (s *Service) AddSchedule(item *domain.Schedule) error {
	if s.schedules == nil {
		return errors.New("jobs: no schedule store configured")
	}
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	item.Enabled = true
	item.CreatedAt, item.UpdatedAt = time.Time{}, time.Time{}
	sch, err := schedmap.ToRecord(item, time.Now(), s.zone, nil)
	if err != nil {
		return err
	}
	if err := s.schedules.CreateSchedule(context.Background(), sch); err != nil {
		return err
	}
	item.CreatedAt, item.UpdatedAt = item.CreatedAt.UTC(), item.UpdatedAt.UTC()
	t := sch.NextRun.UTC()
	item.NextRunAt = &t
	return nil
}

func (s *Service) RemoveSchedule(id string) error {
	if s.schedules == nil {
		return errors.New("jobs: no schedule store configured")
	}
	return s.schedules.DeleteSchedule(context.Background(), id)
}

// ListSchedules returns every schedule (of one repo when repoID is set), ordered by name then id as before.
func (s *Service) ListSchedules(repoID string) ([]domain.Schedule, error) {
	if s.schedules == nil {
		return nil, errors.New("jobs: no schedule store configured")
	}
	all, err := s.schedules.ListSchedules(context.Background())
	if err != nil {
		return nil, err
	}
	out := []domain.Schedule{} // never nil: an empty list encodes as [] not null
	for _, sch := range all {
		item, err := schedmap.FromRecord(sch)
		if err != nil {
			return nil, err
		}
		if repoID == "" || item.RepoID == repoID {
			out = append(out, item)
		}
	}
	sortSchedules(out)
	return out, nil
}

func sortSchedules(items []domain.Schedule) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && less(items[j], items[j-1]); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

func less(a, b domain.Schedule) bool {
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.ID < b.ID
}

// RemoveRepoSchedules removes the schedules of a repo that is being deleted (the old table did this with ON DELETE CASCADE).
func (s *Service) RemoveRepoSchedules(repoID string) error {
	if s.schedules == nil {
		return nil
	}
	items, err := s.ListSchedules(repoID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := s.RemoveSchedule(item.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) TriggerSchedule(ctx context.Context, scheduleID string) (*domain.Job, error) {
	if s.schedules == nil {
		return nil, errors.New("jobs: no schedule store configured")
	}
	sch, ok, err := s.schedules.GetSchedule(ctx, scheduleID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("schedule not found: %s", scheduleID)
	}
	item, err := schedmap.FromRecord(sch)
	if err != nil {
		return nil, err
	}
	return s.trigger(ctx, item.ID, item.JobKind, item.RepoID, item.PayloadJSON, item.MaxAttempts, item.RetryBackoff, item.RetryDelaySecs)
}
