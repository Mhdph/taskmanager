package dashboard

type Summary struct {
	TotalTasks      int64
	CompletedTasks  int64
	OverdueTasks    int64
	UnassignedTasks int64
	TrackedSeconds  int64
}

type StatusStat struct {
	StatusID string

	Name string

	Category string

	TaskCount int64
}

type PriorityStat struct {
	Priority string

	TaskCount int64
}

type AssigneeStat struct {
	UserID *string

	TaskCount int64
}

type Dashboard struct {
	Summary Summary

	ByStatus []StatusStat

	ByPriority []PriorityStat

	ByAssignee []AssigneeStat
}
