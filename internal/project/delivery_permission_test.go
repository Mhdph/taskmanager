package project

import "testing"

func TestTaskDeliveryPermissions(t *testing.T) {
	for _, role := range []MemberRole{MemberRoleManager, MemberRoleMember, MemberRoleViewer} {
		if !roleHasPermission(role, PermissionViewTaskDeliveries) {
			t.Fatalf("%s cannot read deliveries", role)
		}
		for _, permission := range []Permission{PermissionSubmitTaskDelivery, PermissionReviewTaskDelivery} {
			if got := roleHasPermission(role, permission); got != (role != MemberRoleViewer) {
				t.Fatalf("%s %s = %v", role, permission, got)
			}
		}
	}
}
