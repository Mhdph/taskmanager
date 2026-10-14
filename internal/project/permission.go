package project

type Permission string

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
)
