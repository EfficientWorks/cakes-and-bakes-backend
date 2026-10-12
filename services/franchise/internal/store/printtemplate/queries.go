package printtemplate

import "fmt"

const receiptTemplateSelect = `
	SELECT id, branch_id, is_active,
	       show_customer_name, show_order_notes, show_product_comments, show_addon_price,
	       include_customer_receipt, column_alignment, date_time_format,
	       business_name, mobile_no, address, gst_vat_number, business_logo,
	       show_time, text_weight, created_at, updated_at
	  FROM print_receipt_templates WHERE branch_id = $1`

func simpleTemplateSelect(table string) string {
	return fmt.Sprintf(`
		SELECT id, branch_id, is_active, show_time, text_weight, created_at, updated_at
		  FROM %s WHERE branch_id = $1`, table)
}
