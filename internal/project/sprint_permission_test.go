package project

import "testing"

// TestSprintPermissions pins the role policy of the sprint module:
// managers administer sprints, members change the task membership and
// viewers only read them.
func TestSprintPermissions(t *testing.T) {
	for _, tt := range []struct {
		role  MemberRole
		view  bool
		plan  bool
		admin bool
	}{
		{MemberRoleManager, true, true, true},
		{MemberRoleMember, true, true, false},
		{MemberRoleViewer, true, false, false},
	} {
		if got := roleHasPermission(tt.role, PermissionViewSprints); got != tt.view {
			t.Errorf("%s view sprint permission = %v, want %v", tt.role, got, tt.view)
		}

		if got := roleHasPermission(tt.role, PermissionPlanTasks); got != tt.plan {
			t.Errorf("%s plan task permission = %v, want %v", tt.role, got, tt.plan)
		}

		if got := roleHasPermission(tt.role, PermissionManageSprints); got != tt.admin {
			t.Errorf("%s manage sprint permission = %v, want %v", tt.role, got, tt.admin)
		}
	}

	if roleHasPermission(MemberRole("unknown"), PermissionManageSprints) {
		t.Fatal("unknown role can manage sprints")
	}
}
