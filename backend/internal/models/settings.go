package models

import (
	"errors"
	"unicode/utf8"
)

type UserSettings struct {
	AlertDays          []int  `json:"alert_days"`
	NotificationMethod string `json:"notification_method"`
	DefaultCategory    string `json:"default_category"`
}

var validNotificationMethods = map[string]bool{
	"browser": true,
	"none":    true,
}

func (s *UserSettings) Validate() error {
	if len(s.AlertDays) == 0 || len(s.AlertDays) > 5 {
		return errors.New("alert_days must have 1-5 entries")
	}
	for _, d := range s.AlertDays {
		if d < 1 || d > 365 {
			return errors.New("each alert day must be between 1 and 365")
		}
	}
	if !validNotificationMethods[s.NotificationMethod] {
		return errors.New("notification_method must be 'browser' or 'none'")
	}
	if utf8.RuneCountInString(s.DefaultCategory) > 50 {
		return errors.New("default_category must be 50 characters or fewer")
	}
	return nil
}
