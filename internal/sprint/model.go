package sprint

import "time"

// State is the sprint lifecycle state.
type State string

const (
	// StatePlanned is a sprint that has been created but not started.
	StatePlanned State = "planned"

	// StateActive is the one running sprint of a project.
	StateActive State = "active"

	// StateCompleted is a finished sprint. It is immutable.
	StateCompleted State = "completed"
)

// Sprint is a planning container inside one project.
type Sprint struct {
	ID        string
	TenantID  string
	ProjectID string

	Name string
	Goal *string

	PlannedStartAt *time.Time
	PlannedEndAt   time.Time

	State State

	CreatedByUserID string

	StartedAt   *time.Time
	CompletedAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Cursor is the keyset cursor of the sprint list.
type Cursor struct {
	CreatedAt time.Time
	ID        string
}

type ListOptions struct {
	// State filters by lifecycle state. Nil lists every sprint.
	State *State

	Limit  int32
	Cursor *Cursor
}

type SprintPage struct {
	Items      []Sprint
	NextCursor *string
}

// SprintTask is the task view the sprint module needs. The task module
// owns the full task shape; the sprint module only reads the fields that
// drive planning and reporting.
type SprintTask struct {
	ID        string
	TenantID  string
	ProjectID string

	Title string

	EstimateSeconds *int64

	AssigneeUserID *string

	// SprintID is the current sprint assignment; nil means backlog.
	SprintID *string

	Archived  bool
	Completed bool

	CreatedAt time.Time
}

// Totals are the live numbers of a sprint.
type Totals struct {
	TaskCount            int
	EstimatedSeconds     int64
	UnestimatedTaskCount int
}

// ReportTask is one frozen task entry of a completion report.
type ReportTask struct {
	TaskID string

	Title string

	EstimateSeconds *int64

	Archived  bool
	Completed bool

	// CarriedOver marks unfinished work that left the sprint when it
	// completed.
	CarriedOver bool

	// CarriedOverToSprintID is set for unfinished work that moved to
	// another sprint. Nil means the task went back to the backlog.
	CarriedOverToSprintID *string
}

// Report is the sprint summary. Before completion it is computed live from
// the current state; after completion it is the frozen snapshot.
type Report struct {
	SprintID string

	State State

	// Frozen is true for a completed sprint, whose report never changes.
	Frozen bool

	CommitmentTaskCount        int
	CommitmentEstimatedSeconds int64
	CommitmentUnestimatedCount int

	AddedTaskCount        int
	AddedEstimatedSeconds int64

	RemovedTaskCount        int
	RemovedEstimatedSeconds int64

	CompletedTaskCount        int
	CompletedEstimatedSeconds int64

	CarryoverTaskCount        int
	CarryoverEstimatedSeconds int64

	ArchivedTaskCount int

	UnestimatedTaskCount int

	Tasks []ReportTask

	CompletedAt *time.Time
}
