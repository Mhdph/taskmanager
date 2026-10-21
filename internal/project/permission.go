package project

type Permission string

const (
	PermissionViewTaskTime  Permission = "view_task_time"
	PermissionTrackTaskTime Permission = "track_task_time"
)

const (
	PermissionViewProject Permission = "view_project"

	PermissionEditProject Permission = "edit_project"

	PermissionViewMembers Permission = "view_members"

	PermissionManageMembers Permission = "manage_members"

	PermissionManageStatuses Permission = "manage_statuses"

	PermissionArchiveProject Permission = "archive_project"

	PermissionViewStatuses Permission = "view_statuses"

	PermissionCreateTask  Permission = "create_task"
	PermissionViewTasks   Permission = "view_tasks"
	PermissionEditTask    Permission = "edit_task"
	PermissionArchiveTask Permission = "archive_task"

	PermissionViewTaskDependencies   Permission = "view_task_dependencies"
	PermissionManageTaskDependencies Permission = "manage_task_dependencies"

	PermissionViewTaskComments  Permission = "view_task_comments"
	PermissionCreateTaskComment Permission = "create_task_comment"
	PermissionDeleteTaskComment Permission = "delete_task_comment"

	PermissionViewTaskWatchers   Permission = "view_task_watchers"
	PermissionManageTaskWatchers Permission = "manage_task_watchers"

	PermissionViewTaskAttachments Permission = "view_task_attachments"

	PermissionManageTaskAttachments Permission = "manage_task_attachments"

	PermissionViewDashboard Permission = "view_dashboard"

	// PermissionViewSprints allows reading sprints and their reports.
	PermissionViewSprints Permission = "view_sprints"

	// PermissionManageSprints allows creating, editing, starting,
	// completing and deleting sprints.
	PermissionManageSprints Permission = "manage_sprints"

	// PermissionPlanTasks allows changing the sprint a task belongs to.
	PermissionPlanTasks Permission = "plan_tasks"

	// PermissionViewAutomation allows reading a project's automation rules.
	PermissionViewAutomation Permission = "view_automation"

	// PermissionManageAutomation allows creating, editing and deleting
	// automation rules.
	PermissionManageAutomation Permission = "manage_automation"
)
