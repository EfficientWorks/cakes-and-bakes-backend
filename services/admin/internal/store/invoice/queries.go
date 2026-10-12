package invoice

// Every column the row reader expects, in order.
const invoiceColumns = `
	i.id, i.reference, i.kind, i.status,
	i.branch_id, b.name, coalesce(b.email,''), coalesce(b.phone,''),
	coalesce(b.street,''), coalesce(b.city,''), coalesce(b.state,''), coalesce(b.postcode,''),

	i.issued_date, i.message,
	i.bank_name, i.account_number, i.sort_code,

	i.subtotal_cents, i.discount_cents, i.discount_pct,
	i.tax_cents, i.tax_name, i.total_cents,

	i.paid_at, i.sent_at, i.created_at,

	coalesce(i.customer_first_name,''),
	coalesce(i.customer_last_name,''),
	coalesce(i.customer_phone,''),
	coalesce(i.customer_email,''),
	coalesce(i.customer_address,''),
	coalesce(i.customer_city,''),
	coalesce(i.customer_state,''),

	i.is_gift,

	coalesce(i.receiver_first_name,''),
	coalesce(i.receiver_last_name,''),
	coalesce(i.receiver_phone,''),
	coalesce(i.receiver_email,''),
	coalesce(i.receiver_address,''),
	coalesce(i.receiver_city,''),
	coalesce(i.receiver_state,'')
`

const invoiceFrom = `
	FROM invoices i
	JOIN branches b ON b.id = i.branch_id
`
