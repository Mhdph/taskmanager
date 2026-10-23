package member

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // Capacity schedules must work in the minimal runtime image.

	"github.com/mhdph/taskmanager/internal/auth"
)

var (
	ErrInvalidCapacity = errors.New("capacity requires dailyCapacitySeconds (0–86400), unique ISO workingDays (1–7) and a valid IANA timezone")
	ErrInvalidWorkload = errors.New("workload requires YYYY-MM-DD from/to (inclusive, at most 92 days), a valid timezone and limit 1–200")
)

type Capacity struct {
	UserID               string  `json:"userId"`
	Configured           bool    `json:"configured"`
	DailyCapacitySeconds *int64  `json:"dailyCapacitySeconds"`
	WorkingDays          []int16 `json:"workingDays"`
	Timezone             *string `json:"timezone"`
}

type CapacityInput struct {
	Identity             auth.Identity
	UserID               string
	DailyCapacitySeconds int32
	WorkingDays          []int16
	Timezone             string
}

type WorkloadInput struct {
	Identity auth.Identity
	UserID   string
	From     string
	To       string
	Timezone string
	Limit    int
	Cursor   string
}

type WorkloadMetrics struct {
	CapacitySeconds    *int64   `json:"capacitySeconds"`
	PlannedSeconds     int64    `json:"plannedSeconds"`
	AvailableSeconds   *int64   `json:"availableSeconds"`
	OverloadSeconds    *int64   `json:"overloadSeconds"`
	UtilizationPercent *float64 `json:"utilizationPercent"`
}

type WorkloadDay struct {
	Date string `json:"date"`
	WorkloadMetrics
	TaskCount int `json:"taskCount"`
}

type WorkloadMember struct {
	UserID               string          `json:"userId"`
	FirstName            string          `json:"firstName"`
	LastName             string          `json:"lastName"`
	Capacity             Capacity        `json:"capacity"`
	Timezone             string          `json:"timezone"`
	Days                 []WorkloadDay   `json:"days"`
	Totals               WorkloadMetrics `json:"totals"`
	OpenTaskCount        int             `json:"openTaskCount"`
	UnestimatedTaskCount int             `json:"unestimatedTaskCount"`
	UnscheduledTaskCount int             `json:"unscheduledTaskCount"`
	UnscheduledSeconds   int64           `json:"unscheduledSeconds"`
	OverdueTaskCount     int             `json:"overdueTaskCount"`
	OverdueSeconds       int64           `json:"overdueSeconds"`
}

type WorkloadReport struct {
	From       string           `json:"from"`
	To         string           `json:"to"`
	Scope      string           `json:"scope"`
	Items      []WorkloadMember `json:"items"`
	NextCursor *string          `json:"nextCursor"`
}

func normalizeCapacity(input CapacityInput) (CapacityInput, error) {
	if input.UserID == "" || strings.ContainsRune(input.UserID, 0) || input.DailyCapacitySeconds < 0 || input.DailyCapacitySeconds > 86400 || input.WorkingDays == nil || len(input.WorkingDays) > 7 {
		return input, ErrInvalidCapacity
	}
	input.Timezone = strings.TrimSpace(input.Timezone)
	if _, err := capacityLocation(input.Timezone); err != nil {
		return input, ErrInvalidCapacity
	}
	days := append([]int16{}, input.WorkingDays...)
	sort.Slice(days, func(i, j int) bool { return days[i] < days[j] })
	for i, day := range days {
		if day < 1 || day > 7 || (i > 0 && day == days[i-1]) {
			return input, ErrInvalidCapacity
		}
	}
	input.WorkingDays = days
	return input, nil
}

func capacityLocation(zone string) (*time.Location, error) {
	if zone == "" || zone == "Local" || len(zone) > 100 {
		return nil, ErrInvalidCapacity
	}
	return time.LoadLocation(zone)
}

func normalizeWorkload(input WorkloadInput) (WorkloadInput, time.Time, time.Time, error) {
	from, err := time.Parse(time.DateOnly, input.From)
	if err != nil {
		return input, time.Time{}, time.Time{}, ErrInvalidWorkload
	}
	to, err := time.Parse(time.DateOnly, input.To)
	if err != nil || from.Year() < 1 || to.Before(from) || to.Sub(from)/(24*time.Hour) >= 92 || input.Limit < 1 || input.Limit > 200 || strings.ContainsRune(input.Cursor, 0) || strings.ContainsRune(input.UserID, 0) || (input.UserID != "" && input.Cursor != "") {
		return input, time.Time{}, time.Time{}, ErrInvalidWorkload
	}
	if input.Timezone == "" {
		input.Timezone = "Asia/Tehran"
	}
	if _, err := capacityLocation(input.Timezone); err != nil {
		return input, time.Time{}, time.Time{}, ErrInvalidWorkload
	}
	return input, from, to, nil
}

func (s *Service) capacityManager(ctx context.Context, identity auth.Identity) (bool, error) {
	if identity.TenantID == "" || identity.UserID == "" {
		return false, ErrForbidden
	}
	caller, err := s.repository.Get(ctx, identity.TenantID, identity.UserID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return false, err
	}
	return canManageMembers(identity, caller, err == nil), nil
}

func (s *Service) GetCapacity(ctx context.Context, identity auth.Identity, userID string) (Capacity, error) {
	manager, err := s.capacityManager(ctx, identity)
	if err != nil {
		return Capacity{}, err
	}
	if !manager && userID != identity.UserID {
		return Capacity{}, ErrForbidden
	}
	if _, err := s.repository.Get(ctx, identity.TenantID, userID); err != nil {
		return Capacity{}, err
	}
	return s.repository.getCapacity(ctx, identity.TenantID, userID)
}

func (s *Service) SetCapacity(ctx context.Context, input CapacityInput) (Capacity, error) {
	input, err := normalizeCapacity(input)
	if err != nil {
		return Capacity{}, err
	}
	manager, err := s.capacityManager(ctx, input.Identity)
	if err != nil {
		return Capacity{}, err
	}
	if !manager {
		return Capacity{}, ErrForbidden
	}
	return s.repository.setCapacity(ctx, input)
}

func (s *Service) ClearCapacity(ctx context.Context, identity auth.Identity, userID string) error {
	manager, err := s.capacityManager(ctx, identity)
	if err != nil {
		return err
	}
	if !manager {
		return ErrForbidden
	}
	if _, err := s.repository.Get(ctx, identity.TenantID, userID); err != nil {
		return err
	}
	return s.repository.clearCapacity(ctx, identity.TenantID, userID)
}

func (s *Service) Workload(ctx context.Context, input WorkloadInput) (WorkloadReport, error) {
	input, from, to, err := normalizeWorkload(input)
	if err != nil {
		return WorkloadReport{}, err
	}
	manager, err := s.capacityManager(ctx, input.Identity)
	if err != nil {
		return WorkloadReport{}, err
	}
	if !manager && input.UserID != input.Identity.UserID {
		return WorkloadReport{}, ErrForbidden
	}
	report, err := s.repository.workload(ctx, input, from, to, manager)
	if err != nil {
		return WorkloadReport{}, err
	}
	if input.UserID != "" && len(report.Items) == 0 {
		return WorkloadReport{}, ErrNotFound
	}
	return report, nil
}
