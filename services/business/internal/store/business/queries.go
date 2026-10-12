package business

// businessColumns is the column list scanBusiness reads, in order.
const businessColumns = `
	id, name, slug, country_code, currency, timezone, locale, tax_mode, tax_name,
	default_tax_rate_pct::float8, contact_email, contact_phone, status, created_at, updated_at`

const selectBusiness = `SELECT ` + businessColumns + ` FROM businesses`
