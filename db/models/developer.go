package models

import (
	"github.com/g8rswimmer/go-twitter/v2"
	"time"
)

// Developer represents a potential indie developer's Twitter account. This also contains some current metrics for their
// profile.
type Developer struct {
	ID             string
	Name           string
	Username       string
	Description    string
	ProfileCreated time.Time
	PublicMetrics  *twitter.UserMetricsObj `gorm:"embedded;embeddedPrefix:current_"`
}
