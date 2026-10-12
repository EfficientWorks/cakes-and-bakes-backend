package kds

import "errors"

// ErrKOTConflict is a KOT move whose starting status no longer matches.
var ErrKOTConflict = errors.New("kot status changed underneath the request")

// KOTActor is who moved a ticket.
type KOTActor struct {
	ID     *string
	Type   string // staff | system
	Source string // kds | pos | admin | system
	Note   string
}
