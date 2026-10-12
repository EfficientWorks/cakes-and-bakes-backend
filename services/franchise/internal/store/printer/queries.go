package printer

const printerSelect = `
	SELECT id, branch_id, name, kind,
	       COALESCE(ip_address, ''), COALESCE(device_address, ''),
	       is_connected, created_at, updated_at
	  FROM pos_printers`

const printerReturning = `
		RETURNING id, branch_id, name, kind,
		          COALESCE(ip_address, ''), COALESCE(device_address, ''),
		          is_connected, created_at, updated_at`

const kdsPrinterSelect = `
	SELECT id, scope_type, branch_id::text, central_kitchen_id::text, name, kind,
	       COALESCE(ip_address, ''), port, COALESCE(device_address, ''), is_connected, created_at, updated_at
	  FROM kds_printers`

const kdsPrinterReturning = `
	RETURNING id, scope_type, branch_id::text, central_kitchen_id::text, name, kind,
	          COALESCE(ip_address, ''), port, COALESCE(device_address, ''), is_connected, created_at, updated_at`
