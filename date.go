package apicontract

import (
	"encoding/json"
	"fmt"
	"time"
)

// Date is a calendar day with no time or zone, written as YYYY-MM-DD.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// ParseDate parses a YYYY-MM-DD day.
func ParseDate(value string) (Date, error) {
	day, err := time.Parse(dayLayout, value)
	if err != nil {
		return Date{}, err
	}
	return DateOf(day), nil
}

// DateOf returns the day of t in t's location.
func DateOf(t time.Time) Date {
	year, month, day := t.Date()
	return Date{Year: year, Month: month, Day: day}
}

// IsZero reports whether d is the zero Date.
func (d Date) IsZero() bool { return d == Date{} }

// In returns the start of the day in location.
func (d Date) In(location *time.Location) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, location)
}

func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}

func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(d.String())
}

func (d *Date) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*d = Date{}
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	parsed, err := ParseDate(value)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}
