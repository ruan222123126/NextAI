package cron

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	cronv3 "github.com/robfig/cron/v3"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/service/ports"
)

func (s *Service) schedulerTick(now time.Time) ([]string, error) {
	if err := s.validateStore(); err != nil {
		return nil, err
	}

	stateUpdates := map[string]domain.CronJobState{}
	dueJobIDs := make([]string, 0)
	s.deps.Store.ReadCron(func(st ports.CronAggregate) {
		for id, job := range st.Jobs {
			current := st.States[id]
			next := normalizePausedState(current)
			if !jobSchedulable(job, next) {
				next.NextRunAt = nil
				if !stateEqual(current, next) {
					stateUpdates[id] = next
				}
				continue
			}

			nextRunAt, dueAt, err := resolveNextRunAt(job, next.NextRunAt, now)
			if err != nil {
				msg := err.Error()
				next.LastError = &msg
				next.NextRunAt = nil
				if !stateEqual(current, next) {
					stateUpdates[id] = next
				}
				continue
			}

			nextRun := nextRunAt.Format(time.RFC3339)
			next.NextRunAt = &nextRun
			next.LastError = nil
			if dueAt != nil && misfireExceeded(dueAt, runtimeSpec(job), now) {
				failed := statusFailed
				msg := fmt.Sprintf("misfire skipped: scheduled_at=%s", dueAt.Format(time.RFC3339))
				next.LastStatus = &failed
				next.LastError = &msg
				dueAt = nil
			}
			if !stateEqual(current, next) {
				stateUpdates[id] = next
			}
			if dueAt != nil {
				dueJobIDs = append(dueJobIDs, id)
			}
		}
	})

	if len(stateUpdates) == 0 {
		return dueJobIDs, nil
	}
	if err := s.deps.Store.WriteCron(func(st *ports.CronAggregate) error {
		for id, next := range stateUpdates {
			if _, ok := st.Jobs[id]; !ok {
				continue
			}
			st.States[id] = next
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return dueJobIDs, nil
}

func alignStateForMutation(job domain.CronJobSpec, state domain.CronJobState, now time.Time) domain.CronJobState {
	if !jobSchedulable(job, state) {
		state.NextRunAt = nil
		return state
	}
	nextRunAt, _, err := resolveNextRunAt(job, nil, now)
	if err != nil {
		msg := err.Error()
		state.LastError = &msg
		state.NextRunAt = nil
		return state
	}

	nextRunAtText := nextRunAt.Format(time.RFC3339)
	state.NextRunAt = &nextRunAtText
	state.LastError = nil
	return state
}

func normalizePausedState(state domain.CronJobState) domain.CronJobState {
	if !state.Paused && state.LastStatus != nil && *state.LastStatus == statusPaused {
		state.Paused = true
	}
	return state
}

func stateEqual(a, b domain.CronJobState) bool {
	return stringPtrEqual(a.NextRunAt, b.NextRunAt) &&
		stringPtrEqual(a.LastRunAt, b.LastRunAt) &&
		stringPtrEqual(a.LastStatus, b.LastStatus) &&
		stringPtrEqual(a.LastError, b.LastError) &&
		a.Paused == b.Paused
}

func stringPtrEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func jobSchedulable(job domain.CronJobSpec, state domain.CronJobState) bool {
	return job.Enabled && !state.Paused
}

func runtimeSpec(job domain.CronJobSpec) domain.CronRuntimeSpec {
	out := job.Runtime
	if out.MaxConcurrency <= 0 {
		out.MaxConcurrency = 1
	}
	if out.TimeoutSeconds <= 0 {
		out.TimeoutSeconds = 30
	}
	if out.MisfireGraceSeconds < 0 {
		out.MisfireGraceSeconds = 0
	}
	return out
}

func scheduleType(job domain.CronJobSpec) string {
	t := strings.ToLower(strings.TrimSpace(job.Schedule.Type))
	if t == "" {
		return "interval"
	}
	return t
}

func interval(job domain.CronJobSpec) (time.Duration, error) {
	if scheduleType(job) != "interval" {
		return 0, fmt.Errorf("unsupported schedule.type=%q", job.Schedule.Type)
	}

	raw := strings.TrimSpace(job.Schedule.Cron)
	if raw == "" {
		return 0, errors.New("schedule.cron is required for interval jobs")
	}
	if secs, err := strconv.Atoi(raw); err == nil {
		if secs <= 0 {
			return 0, errors.New("schedule interval must be greater than 0")
		}
		return time.Duration(secs) * time.Second, nil
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid schedule interval: %q", raw)
	}
	return parsed, nil
}

func resolveNextRunAt(job domain.CronJobSpec, current *string, now time.Time) (time.Time, *time.Time, error) {
	switch scheduleType(job) {
	case "interval":
		iv, err := interval(job)
		if err != nil {
			return time.Time{}, nil, err
		}
		next, dueAt := resolveIntervalNextRunAt(current, iv, now)
		return next, dueAt, nil
	case "cron":
		schedule, loc, err := expression(job)
		if err != nil {
			return time.Time{}, nil, err
		}
		next, dueAt := resolveExpressionNextRunAt(current, schedule, loc, now)
		return next, dueAt, nil
	default:
		return time.Time{}, nil, fmt.Errorf("unsupported schedule.type=%q", job.Schedule.Type)
	}
}

func expression(job domain.CronJobSpec) (cronv3.Schedule, *time.Location, error) {
	raw := strings.TrimSpace(job.Schedule.Cron)
	if raw == "" {
		return nil, nil, errors.New("schedule.cron is required for cron jobs")
	}

	loc := time.UTC
	if tz := strings.TrimSpace(job.Schedule.Timezone); tz != "" {
		nextLoc, err := time.LoadLocation(tz)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid schedule.timezone=%q", job.Schedule.Timezone)
		}
		loc = nextLoc
	}

	parser := cronv3.NewParser(cronv3.SecondOptional | cronv3.Minute | cronv3.Hour | cronv3.Dom | cronv3.Month | cronv3.Dow | cronv3.Descriptor)
	schedule, err := parser.Parse(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid cron expression: %w", err)
	}
	return schedule, loc, nil
}

func resolveIntervalNextRunAt(current *string, interval time.Duration, now time.Time) (time.Time, *time.Time) {
	next := now.Add(interval)
	if current == nil {
		return next, nil
	}

	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*current))
	if err != nil {
		return next, nil
	}
	if parsed.After(now) {
		return parsed, nil
	}

	dueAt := parsed
	for !parsed.After(now) {
		parsed = parsed.Add(interval)
	}
	return parsed, &dueAt
}

func resolveExpressionNextRunAt(current *string, schedule cronv3.Schedule, loc *time.Location, now time.Time) (time.Time, *time.Time) {
	nowInLoc := now.In(loc)
	next := schedule.Next(nowInLoc).UTC()
	if current == nil {
		return next, nil
	}

	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*current))
	if err != nil {
		return next, nil
	}
	if parsed.After(now) {
		return parsed, nil
	}

	dueAt := parsed
	cursor := parsed.In(loc)
	for i := 0; i < 2048 && !cursor.After(nowInLoc); i++ {
		nextCursor := schedule.Next(cursor)
		if !nextCursor.After(cursor) {
			return schedule.Next(nowInLoc).UTC(), &dueAt
		}
		cursor = nextCursor
	}
	if !cursor.After(nowInLoc) {
		cursor = schedule.Next(nowInLoc)
	}
	return cursor.UTC(), &dueAt
}

func misfireExceeded(dueAt *time.Time, runtime domain.CronRuntimeSpec, now time.Time) bool {
	if dueAt == nil {
		return false
	}
	if runtime.MisfireGraceSeconds <= 0 {
		return false
	}
	grace := time.Duration(runtime.MisfireGraceSeconds) * time.Second
	return now.Sub(dueAt.UTC()) > grace
}
