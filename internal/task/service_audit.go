package task

import (
	"context"
	"log"

	"github.com/mhdph/taskmanager/internal/audit"
)

func (s *Service) recordAudit(ctx context.Context, input audit.RecordInput) {
	if s.auditRecorder == nil {
		return
	}

	if err := s.auditRecorder.Record(ctx, input); err != nil {
		// Audit recording stays best-effort for MVP. A transactional outbox is
		// the future path if core mutations need a durable audit guarantee.
		log.Printf("record audit event %s for %s: %v", input.Action, input.EntityID, err)
	}
}

func auditID(value string) *string {
	return &value
}
