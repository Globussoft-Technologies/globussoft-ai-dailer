// Package callguard enforces TRAI calling-hour regulations.
// Indian telecom law prohibits outbound calls before 9 AM or after 9 PM
// in the organisation's timezone.
package callguard

import (
	"fmt"
	"time"
)

const (
	callStartHour = 9  // 9:00 AM
	callEndHour   = 21 // 9:00 PM
)

// Status is the result of a calling-hours check.
type Status struct {
	Allowed      bool   `json:"allowed"`
	Reason       string `json:"reason"`
	CurrentHour  int    `json:"current_hour"`
	CurrentTime  string `json:"current_time"`
	Timezone     string `json:"timezone"`
	NextAllowed  string `json:"next_allowed,omitempty"`
}

// Check returns whether outbound calls are permitted right now for tzName.
// Falls back to "Asia/Kolkata" on any timezone parse error.
func Check(tzName string) Status {
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc, _ = time.LoadLocation("Asia/Kolkata")
		tzName = "Asia/Kolkata"
	}

	now := time.Now().In(loc)
	hour := now.Hour()
	currentTime := now.Format("03:04 PM")

	switch {
	case hour < callStartHour:
		return Status{
			Allowed:     false,
			Reason:      fmt.Sprintf("Too early — calls allowed only after 9:00 AM. Current time: %s", currentTime),
			CurrentHour: hour,
			CurrentTime: currentTime,
			Timezone:    tzName,
			NextAllowed: "today at 9:00 AM",
		}
	case hour >= callEndHour:
		return Status{
			Allowed:     false,
			Reason:      fmt.Sprintf("Too late — calls allowed only until 9:00 PM. Current time: %s", currentTime),
			CurrentHour: hour,
			CurrentTime: currentTime,
			Timezone:    tzName,
			NextAllowed: "tomorrow at 9:00 AM",
		}
	default:
		return Status{
			Allowed:     true,
			Reason:      "Calling hours active (9 AM – 9 PM)",
			CurrentHour: hour,
			CurrentTime: currentTime,
			Timezone:    tzName,
		}
	}
}

// NextAllowedTime returns a human-readable string for when calls will next be allowed.
func NextAllowedTime(tzName string) string {
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc, _ = time.LoadLocation("Asia/Kolkata")
	}
	hour := time.Now().In(loc).Hour()
	switch {
	case hour < callStartHour:
		return "today at 9:00 AM"
	case hour >= callEndHour:
		return "tomorrow at 9:00 AM"
	default:
		return "now (calling is currently allowed)"
	}
}
