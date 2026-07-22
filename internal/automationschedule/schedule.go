// Package automationschedule implements the deliberately small scheduling
// language accepted by Millena automation rules. It is not a general RRULE
// implementation: unsupported semantics are rejected instead of being
// silently approximated.
package automationschedule

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Frequency string

const (
	Daily   Frequency = "daily"
	Weekly  Frequency = "weekly"
	Monthly Frequency = "monthly"
)

type Spec struct {
	Frequency Frequency
	GapDays   int
	Hour      int
	Minute    int
	Weekday   time.Weekday
	MonthDay  int
}

func (spec Spec) IsGap() bool { return spec.GapDays > 0 }

// Parse accepts gap:Nd (1 <= N <= 365) or this explicit RRULE subset:
//
//   - FREQ=DAILY with optional BYHOUR and BYMINUTE
//   - FREQ=WEEKLY with exactly one BYDAY and optional BYHOUR/BYMINUTE
//   - FREQ=MONTHLY with BYMONTHDAY in 1..28 and optional BYHOUR/BYMINUTE
//
// INTERVAL, COUNT, UNTIL, multiple BYDAY values, duplicate keys and all other
// RRULE fields are rejected because the worker does not implement them.
func Parse(raw string) (Spec, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Spec{}, errors.New("schedule is empty")
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "gap:") {
		if !strings.HasSuffix(lower, "d") {
			return Spec{}, errors.New("gap schedule must end in d")
		}
		value := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(lower, "gap:"), "d"))
		days, err := strconv.Atoi(value)
		if err != nil || days < 1 || days > 365 {
			return Spec{}, errors.New("gap days must be between 1 and 365")
		}
		return Spec{Frequency: Daily, GapDays: days, Hour: 9}, nil
	}

	allowed := map[string]struct{}{
		"FREQ": {}, "BYHOUR": {}, "BYMINUTE": {}, "BYDAY": {}, "BYMONTHDAY": {},
	}
	parts := make(map[string]string)
	for _, rawPart := range strings.Split(strings.ToUpper(raw), ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(rawPart), "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || key == "" || value == "" {
			return Spec{}, errors.New("invalid RRULE field")
		}
		if _, ok := allowed[key]; !ok {
			return Spec{}, fmt.Errorf("unsupported RRULE field %s", key)
		}
		if _, exists := parts[key]; exists {
			return Spec{}, fmt.Errorf("duplicate RRULE field %s", key)
		}
		parts[key] = value
	}

	spec := Spec{Hour: 9}
	switch parts["FREQ"] {
	case "DAILY":
		spec.Frequency = Daily
		if parts["BYDAY"] != "" || parts["BYMONTHDAY"] != "" {
			return Spec{}, errors.New("daily RRULE does not support BYDAY or BYMONTHDAY")
		}
	case "WEEKLY":
		spec.Frequency = Weekly
		if parts["BYDAY"] == "" {
			return Spec{}, errors.New("weekly RRULE requires BYDAY")
		}
		if parts["BYMONTHDAY"] != "" {
			return Spec{}, errors.New("weekly RRULE does not support BYMONTHDAY")
		}
		weekdays := map[string]time.Weekday{
			"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday,
			"WE": time.Wednesday, "TH": time.Thursday, "FR": time.Friday,
			"SA": time.Saturday,
		}
		weekday, ok := weekdays[parts["BYDAY"]]
		if !ok {
			return Spec{}, errors.New("weekly RRULE requires one valid weekday")
		}
		spec.Weekday = weekday
	case "MONTHLY":
		spec.Frequency = Monthly
		if parts["BYDAY"] != "" {
			return Spec{}, errors.New("monthly RRULE does not support BYDAY")
		}
		monthDay, err := strconv.Atoi(parts["BYMONTHDAY"])
		if err != nil || monthDay < 1 || monthDay > 28 {
			return Spec{}, errors.New("monthly RRULE requires BYMONTHDAY between 1 and 28")
		}
		spec.MonthDay = monthDay
	default:
		return Spec{}, errors.New("FREQ must be DAILY, WEEKLY or MONTHLY")
	}

	if value := parts["BYHOUR"]; value != "" {
		hour, err := strconv.Atoi(value)
		if err != nil || hour < 0 || hour > 23 {
			return Spec{}, errors.New("BYHOUR must be an integer between 0 and 23")
		}
		spec.Hour = hour
	}
	if value := parts["BYMINUTE"]; value != "" {
		minute, err := strconv.Atoi(value)
		if err != nil || minute < 0 || minute > 59 {
			return Spec{}, errors.New("BYMINUTE must be an integer between 0 and 59")
		}
		spec.Minute = minute
	}
	return spec, nil
}

func (spec Spec) Next(now time.Time, location *time.Location) time.Time {
	if location == nil {
		location = time.UTC
	}
	localNow := now.In(location)
	if spec.IsGap() {
		return localNow.AddDate(0, 0, 1).UTC()
	}
	// Keep date arithmetic separate from the declared clock. If a wall time is
	// absent during spring-forward, time.Date normalizes that one occurrence;
	// reconstructing from the next calendar date prevents permanent clock drift.
	dateCursor := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 12, 0, 0, 0, location)
	localCandidate := func(date time.Time) time.Time {
		return time.Date(date.Year(), date.Month(), date.Day(), spec.Hour, spec.Minute, 0, 0, location)
	}
	var candidate time.Time
	switch spec.Frequency {
	case Daily:
		candidate = localCandidate(dateCursor)
		if !candidate.After(localNow) {
			dateCursor = dateCursor.AddDate(0, 0, 1)
			candidate = localCandidate(dateCursor)
		}
	case Weekly:
		daysAhead := (int(spec.Weekday) - int(localNow.Weekday()) + 7) % 7
		dateCursor = dateCursor.AddDate(0, 0, daysAhead)
		candidate = localCandidate(dateCursor)
		if !candidate.After(localNow) {
			dateCursor = dateCursor.AddDate(0, 0, 7)
			candidate = localCandidate(dateCursor)
		}
	case Monthly:
		dateCursor = time.Date(localNow.Year(), localNow.Month(), spec.MonthDay, 12, 0, 0, 0, location)
		candidate = localCandidate(dateCursor)
		if !candidate.After(localNow) {
			dateCursor = time.Date(localNow.Year(), localNow.Month()+1, spec.MonthDay, 12, 0, 0, 0, location)
			candidate = localCandidate(dateCursor)
		}
	}
	return candidate.UTC()
}
