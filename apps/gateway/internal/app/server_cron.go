package app

import (
	"time"

	"nextai/apps/gateway/internal/domain"
	cronservice "nextai/apps/gateway/internal/service/cron"
)

func (s *Server) executeCronJob(id string) error {
	return s.getCronService().ExecuteJob(id)
}

func resolveCronNextRunAt(job domain.CronJobSpec, current *string, now time.Time) (time.Time, *time.Time, error) {
	return cronservice.ResolveNextRunAt(job, current, now)
}
