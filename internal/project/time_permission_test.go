package project

import "testing"

func TestTimeTrackingPermissions(t *testing.T) {
	for _, tt := range []struct {
		role  MemberRole
		track bool
	}{
		{MemberRoleManager, true}, {MemberRoleMember, true}, {MemberRoleViewer, false},
	} {
		if !roleHasPermission(tt.role, PermissionViewTaskTime) {
			t.Errorf("%s cannot view tracked time", tt.role)
		}
		if got := roleHasPermission(tt.role, PermissionTrackTaskTime); got != tt.track {
			t.Errorf("%s tracking permission = %v", tt.role, got)
		}
	}
	if roleHasPermission(MemberRole("unknown"), PermissionTrackTaskTime) {
		t.Fatal("unknown role can track time")
	}
}
