package member

import (
	"testing"
	"time"
)

func workloadDate(value string) time.Time {
	day, err := time.Parse(time.DateOnly, value)
	if err != nil {
		panic(err)
	}
	return day
}

func workloadTestCapacity(days []int16, seconds int64) Capacity {
	zone := "UTC"
	return Capacity{UserID: "alice", Configured: true, DailyCapacitySeconds: &seconds, WorkingDays: days, Timezone: &zone}
}

func TestWorkloadDistributesAcrossFullTaskSpan(t *testing.T) {
	start, end := workloadDate("2026-10-19"), workloadDate("2026-10-30")
	capacity := workloadTestCapacity([]int16{1, 2, 3, 4, 5}, 8*3600)
	task := workloadTask{StartAt: &start, DueAt: &end, RemainingSeconds: 41, Estimated: true}
	first := initializeWorkload("alice", "", "", capacity, "UTC", start, start.AddDate(0, 0, 6))
	first.allocate(task, start, start.AddDate(0, 0, 6), time.UTC)
	first.finish()
	secondStart := start.AddDate(0, 0, 7)
	second := initializeWorkload("alice", "", "", capacity, "UTC", secondStart, end)
	second.allocate(task, secondStart, end, time.UTC)
	second.finish()
	if first.Totals.PlannedSeconds != 21 || second.Totals.PlannedSeconds != 20 {
		t.Fatalf("effort lost or duplicated across reports: %d + %d", first.Totals.PlannedSeconds, second.Totals.PlannedSeconds)
	}
	if first.Days[0].PlannedSeconds != 5 || first.Days[5].PlannedSeconds != 0 || *first.Totals.CapacitySeconds != 40*3600 {
		t.Fatalf("workday allocation wrong: %+v", first)
	}
	partialStart, partialEnd := start.AddDate(0, 0, 2), start.AddDate(0, 0, 4)
	partial := initializeWorkload("alice", "", "", capacity, "UTC", partialStart, partialEnd)
	partial.allocate(task, partialStart, partialEnd, time.UTC)
	partial.finish()
	if partial.Totals.PlannedSeconds != 12 {
		t.Fatalf("partial report took whole estimate: %d", partial.Totals.PlannedSeconds)
	}
}

func TestWorkloadOverloadAndBacklogStayVisible(t *testing.T) {
	from, to := workloadDate("2026-10-19"), workloadDate("2026-10-25")
	capacity := workloadTestCapacity([]int16{1, 2, 3, 4, 5}, 8*3600)
	m := initializeWorkload("alice", "", "", capacity, "UTC", from, to)
	offDay := workloadDate("2026-10-24")
	m.allocate(workloadTask{DueAt: &offDay, RemainingSeconds: 16 * 3600, Estimated: true}, from, to, time.UTC)
	m.allocate(workloadTask{RemainingSeconds: 2 * 3600, Estimated: true}, from, to, time.UTC)
	m.allocate(workloadTask{}, from, to, time.UTC)
	overdue := from.AddDate(0, 0, -1)
	m.allocate(workloadTask{DueAt: &overdue, RemainingSeconds: 3 * 3600, Estimated: true}, from, to, time.UTC)
	m.finish()
	if m.Days[5].PlannedSeconds != 16*3600 || *m.Days[5].CapacitySeconds != 0 || *m.Days[5].OverloadSeconds != 16*3600 || m.Days[5].UtilizationPercent != nil {
		t.Fatalf("off-day overload hidden: %+v", m.Days[5])
	}
	if m.UnscheduledTaskCount != 2 || m.UnscheduledSeconds != 2*3600 || m.UnestimatedTaskCount != 1 || m.OverdueSeconds != 3*3600 || m.OverdueTaskCount != 1 {
		t.Fatalf("backlog hidden: %+v", m)
	}
	if *m.Totals.AvailableSeconds != 40*3600 || *m.Totals.OverloadSeconds != 16*3600 {
		t.Fatal("daily free time/overload should be reported independently")
	}
}

func TestUnconfiguredAndZeroCapacity(t *testing.T) {
	from, to := workloadDate("2026-10-19"), workloadDate("2026-10-20")
	task := workloadTask{StartAt: &from, DueAt: &to, RemainingSeconds: 10, Estimated: true}
	m := initializeWorkload("alice", "", "", Capacity{UserID: "alice"}, "UTC", from, to)
	m.allocate(task, from, to, time.UTC)
	m.finish()
	if m.Totals.CapacitySeconds != nil || m.Totals.AvailableSeconds != nil || m.Totals.UtilizationPercent != nil || m.Totals.PlannedSeconds != 10 {
		t.Fatalf("unset capacity guessed a schedule: %+v", m)
	}
	zero := initializeWorkload("alice", "", "", workloadTestCapacity([]int16{}, 0), "UTC", from, to)
	zero.allocate(task, from, to, time.UTC)
	zero.finish()
	if *zero.Totals.OverloadSeconds != 10 || *zero.Totals.CapacitySeconds != 0 || zero.Totals.UtilizationPercent != nil {
		t.Fatal("zero capacity treated as unconfigured or dropped work")
	}
}

func TestWorkloadCalendarHandlesTimezoneAndDST(t *testing.T) {
	zone, err := capacityLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 31, 12, 0, 0, 0, zone)
	end := time.Date(2026, 11, 2, 12, 0, 0, 0, zone)
	from, to := workloadDate("2026-10-31"), workloadDate("2026-11-02")
	m := initializeWorkload("alice", "", "", workloadTestCapacity([]int16{1, 2, 3, 4, 5, 6, 7}, 8*3600), "UTC", from, to)
	m.allocate(workloadTask{StartAt: &start, DueAt: &end, RemainingSeconds: 30, Estimated: true}, from, to, zone)
	m.finish()
	if len(m.Days) != 3 || m.Days[0].PlannedSeconds != 10 || m.Days[1].PlannedSeconds != 10 || *m.Totals.CapacitySeconds != 24*3600 {
		t.Fatal("DST changed calendar-day allocation")
	}
	utc := time.Date(2026, 11, 1, 3, 30, 0, 0, time.UTC)
	if got := civilDay(utc, zone).Format(time.DateOnly); got != "2026-10-31" {
		t.Fatalf("UTC was used as local date: %s", got)
	}
}

func TestWorkingDayCountLongSpans(t *testing.T) {
	var all [8]bool
	for day := 1; day <= 7; day++ {
		all[day] = true
	}
	start, end := workloadDate("0100-01-01"), workloadDate("9998-12-31")
	want := (end.Unix()-start.Unix())/86400 + 1
	if got := workingDayCount(start, end, all); got != want {
		t.Fatalf("long span overflow: %d want %d", got, want)
	}
}
