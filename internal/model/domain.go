package model

import (
	"github.com/google/uuid"
)

type UserRole string

const (
	UserRoleAkimatAdmin     UserRole = "AKIMAT_ADMIN"
	UserRoleKguZkhAdmin     UserRole = "KGU_ZKH_ADMIN"
	UserRoleTooAdmin        UserRole = "TOO_ADMIN"
	UserRoleContractorAdmin UserRole = "CONTRACTOR_ADMIN"
	UserRoleDriver          UserRole = "DRIVER"
)

type Principal struct {
	UserID   uuid.UUID
	OrgID    uuid.UUID
	Role     UserRole
	DriverID *uuid.UUID
}

func (p Principal) IsAkimat() bool {
	return p.Role == UserRoleAkimatAdmin
}

func (p Principal) IsKgu() bool {
	return p.Role == UserRoleKguZkhAdmin
}

func (p Principal) IsToo() bool {
	return p.Role == UserRoleTooAdmin
}

func (p Principal) IsContractor() bool {
	return p.Role == UserRoleContractorAdmin
}

func (p Principal) IsDriver() bool {
	return p.Role == UserRoleDriver
}

