package member

import "time"

type workloadTask struct {
	StartAt          *time.Time
	DueAt            *time.Time
	RemainingSeconds int64
	Estimated        bool
}

func civilDay(value time.Time, zone *time.Location) time.Time {
	year, month, day := value.In(zone).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func isoWeekday(day time.Time) int { return (int(day.Weekday())+6)%7 + 1 }

// Count whole weeks and at most six extra days, including for long task spans.
func workingDayCount(from, to time.Time, working [8]bool) int64 {
	if to.Before(from) {
		return 0
	}
	n := (to.Unix()-from.Unix())/86400 + 1
	var perWeek int64
	for _, yes := range working {
		if yes {
			perWeek++
		}
	}
	count := (n / 7) * perWeek
	for i := int64(0); i < n%7; i++ {
		if working[isoWeekday(from.AddDate(0, 0, int(i)))] {
			count++
		}
	}
	return count
}

func initializeWorkload(userID, firstName, lastName string, capacity Capacity, fallbackZone string, from, to time.Time) WorkloadMember {
	zone := fallbackZone
	if capacity.Timezone != nil {
		zone = *capacity.Timezone
	}
	result := WorkloadMember{UserID: userID, FirstName: firstName, LastName: lastName, Capacity: capacity, Timezone: zone, Days: []WorkloadDay{}}
	var working [8]bool
	for _, day := range capacity.WorkingDays {
		working[day] = true
	}
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		entry := WorkloadDay{Date: day.Format(time.DateOnly)}
		if capacity.Configured {
			seconds := int64(0)
			if working[isoWeekday(day)] {
				seconds = *capacity.DailyCapacitySeconds
			}
			entry.CapacitySeconds = &seconds
		}
		result.Days = append(result.Days, entry)
	}
	return result
}

func (m *WorkloadMember) allocate(task workloadTask, from, to time.Time, zone *time.Location) {
	var first, last time.Time
	switch {
	case task.StartAt != nil && task.DueAt != nil:
		first, last = civilDay(*task.StartAt, zone), civilDay(*task.DueAt, zone)
		if last.Before(first) {
			first = last
		}
	case task.DueAt != nil:
		first = civilDay(*task.DueAt, zone)
		last = first
	case task.StartAt != nil:
		first = civilDay(*task.StartAt, zone)
		last = first
	}
	hasDate := task.StartAt != nil || task.DueAt != nil
	if hasDate && first.After(to) {
		return
	}
	m.OpenTaskCount++
	if !task.Estimated {
		m.UnestimatedTaskCount++
	}
	if !hasDate || (task.DueAt == nil && last.Before(from)) {
		m.UnscheduledTaskCount++
		m.UnscheduledSeconds += task.RemainingSeconds
		return
	}
	if last.Before(from) {
		m.OverdueTaskCount++
		m.OverdueSeconds += task.RemainingSeconds
		return
	}
	var working [8]bool
	if m.Capacity.Configured {
		for _, day := range m.Capacity.WorkingDays {
			working[day] = true
		}
	} else {
		for day := 1; day <= 7; day++ {
			working[day] = true
		}
	}
	count := workingDayCount(first, last, working)
	if count == 0 {
		// Keep off-day assignments visible as overload instead of dropping them.
		for day := 1; day <= 7; day++ {
			working[day] = true
		}
		count = workingDayCount(first, last, working)
	}
	for i := range m.Days {
		day := from.AddDate(0, 0, i)
		if day.Before(first) || day.After(last) || !working[isoWeekday(day)] {
			continue
		}
		index := workingDayCount(first, day.AddDate(0, 0, -1), working)
		seconds := task.RemainingSeconds / count
		if index < task.RemainingSeconds%count {
			seconds++
		}
		m.Days[i].PlannedSeconds += seconds
		m.Days[i].TaskCount++
	}
}

func finishMetrics(metrics *WorkloadMetrics) {
	if metrics.CapacitySeconds == nil {
		return
	}
	available := max(*metrics.CapacitySeconds-metrics.PlannedSeconds, 0)
	overload := max(metrics.PlannedSeconds-*metrics.CapacitySeconds, 0)
	metrics.AvailableSeconds = &available
	metrics.OverloadSeconds = &overload
	if *metrics.CapacitySeconds > 0 {
		percent := 100 * float64(metrics.PlannedSeconds) / float64(*metrics.CapacitySeconds)
		metrics.UtilizationPercent = &percent
	}
}

func (m *WorkloadMember) finish() {
	if m.Capacity.Configured {
		capacity := int64(0)
		available := int64(0)
		overload := int64(0)
		m.Totals.CapacitySeconds = &capacity
		m.Totals.AvailableSeconds = &available
		m.Totals.OverloadSeconds = &overload
	}
	for i := range m.Days {
		finishMetrics(&m.Days[i].WorkloadMetrics)
		m.Totals.PlannedSeconds += m.Days[i].PlannedSeconds
		if m.Capacity.Configured {
			*m.Totals.CapacitySeconds += *m.Days[i].CapacitySeconds
			*m.Totals.AvailableSeconds += *m.Days[i].AvailableSeconds
			*m.Totals.OverloadSeconds += *m.Days[i].OverloadSeconds
		}
	}
	if m.Totals.CapacitySeconds != nil && *m.Totals.CapacitySeconds > 0 {
		percent := 100 * float64(m.Totals.PlannedSeconds) / float64(*m.Totals.CapacitySeconds)
		m.Totals.UtilizationPercent = &percent
	}
}
