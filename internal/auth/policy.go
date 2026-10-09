package auth

const (
	RoleAdmin  Role = "Admin"
	RoleMember Role = "Member"
)

func (i Identity) IsAdmin() bool {
	return i.Role == RoleAdmin
}
