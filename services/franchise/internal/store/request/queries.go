package request

const branchRequestSelect = `
	SELECT
		br.id, br.branch_id,
		br.requested_by, br.requested_at, br.role,
		br.type, br.title, br.description, br.fields_to_change, br.status,
		br.approved_at, br.approved_by,
		br.rejected_at, br.rejected_by, br.rejection_note,
		br.completed_at, br.completed_by,
		br.created_at, br.updated_at,
		br.modifier_type, br.modifier_name,
		COALESCE(b.name, '') AS branch_name
	FROM branch_requests br
	LEFT JOIN branches b ON b.id = br.branch_id`
