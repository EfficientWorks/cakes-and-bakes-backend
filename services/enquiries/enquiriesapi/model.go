// Package enquiriesapi is the enquiries service's published contract: the
// enquiry and newsletter records it owns. It holds no logic and imports
// nothing from the service itself.
package enquiriesapi

import "time"

// Enquiry types. The three storefront forms share one lifecycle and one admin
// queue, so they share one record and differ only in Details (SF-34).
const (
	// TypeCustomOrder is the "Need a Custom Order?" form: an event, a guest
	// count and a chosen store.
	TypeCustomOrder = "custom_order"
	// TypeFranchise is the "Own a franchise" enquiry, which is brand-level
	// and never routed to a branch.
	TypeFranchise = "franchise"
	// TypeContact is the general contact form.
	TypeContact = "contact"
)

// Enquiry lifecycle.
const (
	StatusNew        = "new"
	StatusInProgress = "in_progress"
	StatusClosed     = "closed"
)

// Enquiry is one submitted form.
type Enquiry struct {
	ID      string  `json:"id"`
	Type    string  `json:"type" example:"custom_order"`
	Name    string  `json:"name"`
	Email   string  `json:"email"`
	Phone   *string `json:"phone,omitempty"`
	Message *string `json:"message,omitempty"`

	// BranchID is set when the form names a store, which is what routes the
	// enquiry to the right inbox. It is nil for franchise and general contact
	// enquiries, which head office answers.
	BranchID *string `json:"branchId,omitempty"`

	// Details carries the fields specific to the form: event date, event type,
	// guest count, interested location, and so on.
	Details map[string]any `json:"details"`

	Status    string     `json:"status" example:"new"`
	HandledBy *string    `json:"handledBy,omitempty"`
	HandledAt *time.Time `json:"handledAt,omitempty"`
	StaffNote *string    `json:"staffNote,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

// EnquiryFilter drives the admin queue.
type EnquiryFilter struct {
	Type     string
	Status   string
	BranchID string
	Search   string
	Page     int
	Limit    int
}

// Page-size bounds, matching the API conventions.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// Normalise clamps paging so the store and the handler agree.
func (f EnquiryFilter) Normalise() EnquiryFilter {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit <= 0 {
		f.Limit = DefaultPageSize
	}
	if f.Limit > MaxPageSize {
		f.Limit = MaxPageSize
	}
	return f
}

// Offset is the SQL offset for the filter's page.
func (f EnquiryFilter) Offset() int { return (f.Page - 1) * f.Limit }

// NewsletterSubscriber records marketing consent. Unsubscribing sets
// UnsubscribedAt rather than deleting the row, so a later re-subscribe is
// still an auditable consent trail (NOT-06).
type NewsletterSubscriber struct {
	ID             string     `json:"id"`
	Email          string     `json:"email"`
	Source         string     `json:"source" example:"footer"`
	ConsentedAt    time.Time  `json:"consentedAt"`
	UnsubscribedAt *time.Time `json:"unsubscribedAt,omitempty"`
}

// Subscribed reports whether the consent is currently active.
func (n NewsletterSubscriber) Subscribed() bool { return n.UnsubscribedAt == nil }
