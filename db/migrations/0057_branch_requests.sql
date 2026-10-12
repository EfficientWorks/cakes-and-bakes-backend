-- +goose Up

-- Branch requests track an intent by branch-scoped or franchise-scoped staff
-- to perform some operation on a branch. A Super Admin reviews each request
-- and changes its status. Lifecycle fields are stored inline; the audit_log
-- table covers compliance-level history via the domain audit writes.
CREATE TABLE branch_requests (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id       UUID NOT NULL REFERENCES branches(id),
    franchise_id    UUID NOT NULL REFERENCES franchises(id),
    fields_to_change TEXT[] NULL,
    
-- Who made the request and when
requested_by UUID NOT NULL REFERENCES staff_users (id),
requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
role TEXT NOT NULL, -- requester''s role at creation time

-- What they want
type TEXT NOT NULL, -- free-text category e.g. "menu_change"
title TEXT NOT NULL,
description TEXT NOT NULL DEFAULT '',
modifier_type TEXT,
modifier_name TEXT,

-- Lifecycle status: pending | approved | rejected | cancelled | completed
status TEXT NOT NULL DEFAULT 'pending',

-- Approval (set by super_admin)
approved_at TIMESTAMPTZ,
approved_by UUID REFERENCES staff_users (id),

-- Rejection (set by super_admin)
rejected_at TIMESTAMPTZ,
rejected_by UUID REFERENCES staff_users (id),
rejection_note TEXT,

-- Completion (work done after approval)


completed_at    TIMESTAMPTZ,
    completed_by    UUID REFERENCES staff_users(id),

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_branch_requests_branch ON branch_requests (branch_id);

CREATE INDEX idx_branch_requests_franchise ON branch_requests (franchise_id);

CREATE INDEX idx_branch_requests_status ON branch_requests (status);

CREATE INDEX idx_branch_requests_requester ON branch_requests (requested_by);

-- +goose Down
DROP TABLE IF EXISTS branch_requests;