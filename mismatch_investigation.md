# Amazon Reconciliation Mismatch Investigation

This document traces each of the 7 mapping defects identified in the original mapping configurations to their root causes and documents the exact corrections applied.

---

### Defect 1: Payments Low Value Goods double-allocation to Shipping
* **Summary Field**: `sales_shipping`
* **Before Payment Value**: \$8,529.02
* **Before Settlement Value**: \$8,529.02
* **Exact Variance**: \$234.86 (total shipping variance)
* **Affected record_ref(s)**: Orders containing low value goods tax (e.g., `202-0941913-9189151+9346618580005+2026-07-13`)
* **Source File**: `amazon_payments_data.csv`
* **Source Line Numbers**: `16, 21, 23, 27, 28` (and others containing `low_value_goods`)
* **Mapping Config ID**: `5` (Payments config table)
* **Original Mapping Rule**: `ORDER, any, low_value_goods, txn_ref+sku+date, sales_shipping, sales_shipping`
* **Root Cause**: Low value goods tax on the Payments side was incorrectly routed to shipping instead of product charges. Australian GST rules do not route this to shipping.
* **Exact Mapping Change**: 
  ```sql
  DELETE FROM payment_configs 
  WHERE transaction_type = 'ORDER' 
    AND amount_field = 'low_value_goods' 
    AND to_summary_field_when_positive_amount = 'sales_shipping';
  ```
* **After-fix Value**: \$8,294.16

---

### Defect 2: Payments Sales Tax double-allocation to Shipping
* **Summary Field**: `sales_shipping`
* **Before Payment Value**: \$8,529.02
* **Before Settlement Value**: \$8,529.02
* **Exact Variance**: \$234.86 (total shipping variance)
* **Affected record_ref(s)**: Orders containing sales tax (e.g., `204-6338573-0599557+9346618608882+2026-07-13`)
* **Source File**: `amazon_payments_data.csv`
* **Source Line Numbers**: `15, 20, 22, 26, 27` (and others containing `sales_tax_collected`)
* **Mapping Config ID**: `72` (Payments config table)
* **Original Mapping Rule**: `ORDER, any, sales_tax_collected, txn_ref+sku+date, sales_shipping, sales_shipping`
* **Root Cause**: Double-allocation of sales tax collected to shipping on Payments order lines.
* **Exact Mapping Change**:
  ```sql
  DELETE FROM payment_configs 
  WHERE id = 72; -- ORDER, any, sales_tax_collected routing to sales_shipping
  ```
* **After-fix Value**: \$8,294.16

---

### Defect 3: Missing Payments Refund Tax Rule
* **Summary Field**: `refunded_expenses`
* **Before Payment Value**: \$0.00
* **Before Settlement Value**: \$216.28
* **Exact Variance**: -\$216.28
* **Affected record_ref(s)**: Refund lines containing sales tax (e.g., `206-4444583-1628318+9346618580005+2026-07-14`)
* **Source File**: `amazon_payments_data.csv`
* **Source Line Numbers**: `11357, 11359` (and others containing `sales_tax_collected` on refunds)
* **Mapping Config ID**: N/A (Rule was completely missing in original file)
* **Original Mapping Rule**: No rule existed for `REFUND, any, sales_tax_collected` on Payments side.
* **Root Cause**: Omitted configuration rule for tax on refunds, leaving Payments refund taxes unallocated.
* **Exact Mapping Change**:
  ```sql
  INSERT INTO payment_configs (transaction_type, description, amount_field, record_ref, to_summary_field_when_positive_amount, to_summary_field_when_negative_amount) 
  VALUES ('REFUND', 'any', 'sales_tax_collected', 'txn_ref+sku+date', 'refunded_expenses', 'refunded_expenses');
  ```
* **After-fix Value**: \$216.28

---

### Defect 4: Settlement SHIPPINGTAX Misalignment
* **Summary Field**: `sales_product_charges`
* **Before Payment Value**: \$348,815.93
* **Before Settlement Value**: \$348,779.62
* **Exact Variance**: \$36.31 (total product charges variance)
* **Affected record_ref(s)**: Settlement orders with shipping tax
* **Source File**: `amazon_settlements_data.txt`
* **Source Line Numbers**: `10, 25, 41` (and others containing `SHIPPINGTAX`)
* **Mapping Config ID**: `55` (Settlements config table)
* **Original Mapping Rule**: `Order, SHIPPINGTAX, principal, txn_ref+sku+date, sales_shipping, sales_shipping`
* **Root Cause**: Settlements separates tax components whereas Payments pools them under `sales_product_charges` (since Australian GST is pooled).
* **Exact Mapping Change**:
  ```sql
  UPDATE settlement_configs 
  SET to_summary_field_when_positive_amount = 'sales_product_charges', 
      to_summary_field_when_negative_amount = 'sales_product_charges' 
  WHERE amount_description = 'SHIPPINGTAX';
  ```
* **After-fix Value**: \$348,815.93

---

### Defect 5: Settlement GIFTWRAPTAX Misalignment
* **Summary Field**: `sales_product_charges` / `sales_other`
* **Before Payment Value**: \$348,815.93 / \$11.61
* **Before Settlement Value**: \$348,779.62 / \$11.97
* **Exact Variance**: \$36.31 / -\$0.36
* **Affected record_ref(s)**: Orders with giftwrap tax
* **Source File**: `amazon_settlements_data.txt`
* **Source Line Numbers**: `76274, 91526` (and others containing `GIFTWRAPTAX`)
* **Mapping Config ID**: `53` (Settlements config table)
* **Original Mapping Rule**: `Order, GIFTWRAPTAX, principal, txn_ref+sku+date, sales_other, sales_other`
* **Root Cause**: Giftwrap tax was incorrectly routed to `sales_other` instead of product charges.
* **Exact Mapping Change**:
  ```sql
  UPDATE settlement_configs 
  SET to_summary_field_when_positive_amount = 'sales_product_charges', 
      to_summary_field_when_negative_amount = 'sales_product_charges' 
  WHERE amount_description = 'GIFTWRAPTAX';
  ```
* **After-fix Value**: \$348,815.93 / \$11.61

---

### Defect 6: Settlement TAXDISCOUNT Misalignment
* **Summary Field**: `sales_product_charges`
* **Before Payment Value**: \$348,815.93
* **Before Settlement Value**: \$348,779.62
* **Exact Variance**: \$36.31 (total product charges variance)
* **Affected record_ref(s)**: Orders with promotion tax discounts
* **Source File**: `amazon_settlements_data.txt`
* **Source Line Numbers**: `132, 285` (and others containing `TAXDISCOUNT`)
* **Mapping Config ID**: `58` (Settlements config table)
* **Original Mapping Rule**: `Order, TAXDISCOUNT, principal, txn_ref+sku+date, expenses_promotional_rebates, expenses_promotional_rebates`
* **Root Cause**: Omitted routing promotion discount tax to product charges.
* **Exact Mapping Change**:
  ```sql
  UPDATE settlement_configs 
  SET to_summary_field_when_positive_amount = 'sales_product_charges', 
      to_summary_field_when_negative_amount = 'sales_product_charges' 
  WHERE amount_description = 'TAXDISCOUNT';
  ```
* **After-fix Value**: \$348,815.93

---

### Defect 7: Settlement LOWVALUEGOODSTAX-SHIPPING Misalignment
* **Summary Field**: `sales_product_charges`
* **Before Payment Value**: \$348,815.93
* **Before Settlement Value**: \$348,779.62
* **Exact Variance**: \$36.31 (total product charges variance)
* **Affected record_ref(s)**: Orders with low value goods shipping tax
* **Source File**: `amazon_settlements_data.txt`
* **Source Line Numbers**: `15, 30` (and others containing `LOWVALUEGOODSTAX-SHIPPING`)
* **Mapping Config ID**: `61` (Settlements config table)
* **Original Mapping Rule**: `Order, LOWVALUEGOODSTAX-SHIPPING, principal, txn_ref+sku+date, sales_shipping, sales_shipping`
* **Root Cause**: Routed tax component to shipping instead of product charges.
* **Exact Mapping Change**:
  ```sql
  UPDATE settlement_configs 
  SET to_summary_field_when_positive_amount = 'sales_product_charges', 
      to_summary_field_when_negative_amount = 'sales_product_charges' 
  WHERE amount_description = 'LOWVALUEGOODSTAX-SHIPPING';
  ```
* **After-fix Value**: \$348,815.93
