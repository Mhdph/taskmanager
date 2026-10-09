package project

type Permission string

const (
	PermissionViewProject Permission = "view_project"

	PermissionEditProject Permission = "edit_project"

	PermissionManageMembers Permission = "manage_members"

	PermissionManageStatuses Permission = "manage_statuses"

	PermissionArchiveProject Permission = "archive_project"
)
