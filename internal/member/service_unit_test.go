package member

import (
	"testing"

	"github.com/mhdph/taskmanager/internal/auth"
)

func TestRoleValid(t *testing.T) {
	tests := []struct {
		role Role
		want bool
	}{
		{RoleOwner, true},
		{RoleAdmin, true},
		{RoleMember, true},
		{RoleGuest, true},
		{Role("manager"), false},
		{Role(""), false},
		{Role("OWNER"), false},
	}

	for _, test := range tests {
		if got := test.role.Valid(); got != test.want {
			t.Errorf("Role(%q).Valid() = %v, want %v", test.role, got, test.want)
		}
	}
}

func TestStatusValid(t *testing.T) {
	tests := []struct {
		status Status
		want   bool
	}{
		{StatusActive, true},
		{StatusInvited, true},
		{Status("banned"), false},
		{Status(""), false},
	}

	for _, test := range tests {
		if got := test.status.Valid(); got != test.want {
			t.Errorf("Status(%q).Valid() = %v, want %v", test.status, got, test.want)
		}
	}
}

func TestGuardLastOwner(t *testing.T) {
	count := func(n int64) func() (int64, error) {
		return func() (int64, error) {
			return n, nil
		}
	}

	tests := []struct {
		name        string
		currentRole string
		nextRole    string
		owners      int64
		wantErr     error
	}{
		{
			name:        "demote last owner is rejected",
			currentRole: "owner",
			nextRole:    "member",
			owners:      1,
			wantErr:     ErrLastOwner,
		},
		{
			name:        "remove last owner is rejected",
			currentRole: "owner",
			nextRole:    "",
			owners:      1,
			wantErr:     ErrLastOwner,
		},
		{
			name:        "demote one of several owners is allowed",
			currentRole: "owner",
			nextRole:    "member",
			owners:      2,
			wantErr:     nil,
		},
		{
			name:        "owner to owner is a no-op",
			currentRole: "owner",
			nextRole:    "owner",
			owners:      1,
			wantErr:     nil,
		},
		{
			name:        "demote non-owner never consults the guard",
			currentRole: "member",
			nextRole:    "guest",
			owners:      0,
			wantErr:     nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := guardLastOwner(test.currentRole, test.nextRole, count(test.owners))
			if err != test.wantErr {
				t.Fatalf("guardLastOwner() = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestCanManageMembers(t *testing.T) {
	tests := []struct {
		name      string
		identity  auth.Identity
		member    Member
		hasMember bool
		want      bool
	}{
		{
			name:      "plain member cannot manage",
			identity:  auth.Identity{Role: auth.RoleMember},
			hasMember: true,
			member:    Member{Role: RoleMember},
			want:      false,
		},
		{
			name:      "hosted admin can manage",
			identity:  auth.Identity{Role: auth.RoleAdmin},
			hasMember: false,
			want:      true,
		},
		{
			name:      "support user can manage",
			identity:  auth.Identity{Role: auth.RoleMember, IsSupport: true},
			hasMember: false,
			want:      true,
		},
		{
			name:      "directory owner can manage",
			identity:  auth.Identity{Role: auth.RoleMember},
			hasMember: true,
			member:    Member{Role: RoleOwner},
			want:      true,
		},
		{
			name:      "directory admin can manage",
			identity:  auth.Identity{Role: auth.RoleMember},
			hasMember: true,
			member:    Member{Role: RoleAdmin},
			want:      true,
		},
		{
			name:      "directory guest cannot manage",
			identity:  auth.Identity{Role: auth.RoleMember},
			hasMember: true,
			member:    Member{Role: RoleGuest},
			want:      false,
		},
		{
			name:      "caller without membership cannot manage",
			identity:  auth.Identity{Role: auth.RoleMember},
			hasMember: false,
			want:      false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := canManageMembers(test.identity, test.member, test.hasMember); got != test.want {
				t.Fatalf("canManageMembers() = %v, want %v", got, test.want)
			}
		})
	}
}
