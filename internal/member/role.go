package member

type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleGuest  Role = "guest"
)

func (r Role) Valid() bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleMember, RoleGuest:
		return true
	}

	return false
}

type Status string

const (
	StatusActive  Status = "active"
	StatusInvited Status = "invited"
)

func (s Status) Valid() bool {
	switch s {
	case StatusActive, StatusInvited:
		return true
	}

	return false
}
