package task

import (
	"testing"
	"time"

	"github.com/mhdph/taskmanager/internal/project"
)

func TestCompletedAtForStatusChange(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	previous := now.Add(-time.Hour)
	tests := []struct {
		name       string
		current    *time.Time
		changed    bool
		category   project.StatusCategory
		wantUpdate bool
		want       *time.Time
	}{
		{name: "enter done", changed: true, category: project.StatusCategoryDone, wantUpdate: true, want: &now},
		{name: "already completed entering done", current: &previous, changed: true, category: project.StatusCategoryDone},
		{name: "leave done", current: &previous, changed: true, category: project.StatusCategoryTodo, wantUpdate: true},
		{name: "unrelated update", current: &previous, category: project.StatusCategoryDone},
		{name: "remain incomplete", changed: true, category: project.StatusCategoryTodo},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			update, got := completedAtForStatusChange(tt.current, tt.changed, tt.category, now)
			if update != tt.wantUpdate {
				t.Fatalf("update=%v want %v", update, tt.wantUpdate)
			}
			if (got == nil) != (tt.want == nil) {
				t.Fatalf("completedAt=%v want %v", got, tt.want)
			}
			if got != nil && !got.Equal(*tt.want) {
				t.Fatalf("completedAt=%v want %v", got, tt.want)
			}
		})
	}
}

func TestCompletedAtForCreate(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	if got := completedAtForCreate(project.StatusCategoryDone, now); got == nil || !got.Equal(now) {
		t.Fatalf("done create completedAt=%v, want %v", got, now)
	}
	if got := completedAtForCreate(project.StatusCategoryTodo, now); got != nil {
		t.Fatalf("todo create completedAt=%v, want nil", got)
	}
}
