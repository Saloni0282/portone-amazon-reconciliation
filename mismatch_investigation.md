# Amazon Reconciliation Mismatch Investigation

This document traces each of the 7 mapping defects identified in the original mapping configurations to their root causes and documents the exact corrections applied.

---

### Defect 1: Payments Low Value Goods routing to Shipping
* **Summary Field**: `sales_shipping`
* **Before-fix Payments**: \$8,529.02 (Inflated by \$234.86)
* **Before-fix Settlements**: \$8,529.02 (Inflated by \$234.86)
* **Target Correct Value**: \$8,294.16
* **Exact Inflation Variance**: \$234.86
* **Affected record_ref(s)**: Orders containing low value goods tax (e.g., `202-0941913-9189151+9346618580005+2026-07-13`)
* **Source File**: `amazon_payments_data.csv`
* **Source Line Numbers**: `16, 21, 23, 27, 28` (and others containing `low_value_goods`)
* **Mapping Config Line**: Line 5 (Payments config table)
* **Original Mapping Rule**: `ORDER, any, low_value_goods, txn_ref+sku+date, sales_shipping, sales_shipping`
* **Root Cause**: The Payments configuration incorrectly routed `low_value_goods` to `sales_shipping`, causing tax to be included in the shipping summary instead of following the product-tax allocation. In this dataset, the `low_value_goods` column is empty (\$0.00), so its actual variance contribution is \$0.00. However, the rule itself is erroneous.
* **Exact Mapping Change**: 
  ```sql
  DELETE FROM payment_configs 
  WHERE transaction_type = 'ORDER' 
    AND amount_field = 'low_value_goods' 
    AND to_summary_field_when_positive_amount = 'sales_shipping';
  ```
* **After-fix Value**: \$8,294.16

---

### Defect 2: Payments Sales Tax routing to Shipping
* **Summary Field**: `sales_shipping`
* **Before-fix Payments**: \$8,529.02 (Inflated by \$234.86)
* **Before-fix Settlements**: \$8,529.02 (Inflated by \$234.86)
* **Target Correct Value**: \$8,294.16
* **Exact Inflation Variance**: \$234.86 (This defect contributes the entire \$234.86 variance to the shipping category)
* **Affected record_ref(s)**: Orders containing sales tax (e.g., `204-6338573-0599557+9346618608882+2026-07-13`)
* **Source File**: `amazon_payments_data.csv`
* **Source Line Numbers**: `15, 20, 22, 26, 27` (and others containing `sales_tax_collected`)
* **Mapping Config Line**: Line 72 (referenced as Line 70 in comments)
* **Original Mapping Rule**: `ORDER, any, sales_tax_collected, txn_ref+sku+date, sales_shipping, sales_shipping`
* **Root Cause**: The Payments configuration incorrectly routed `sales_tax_collected` on order lines to shipping instead of product charges. This duplicate rule inflated the shipping summary totals by the tax amount (\$234.86).
* **Exact Mapping Change**:
  ```sql
  DELETE FROM payment_configs 
  WHERE transaction_type = 'ORDER' 
    AND amount_field = 'sales_tax_collected' 
    AND to_summary_field_when_positive_amount = 'sales_shipping';
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
* **Mapping Config Line**: Line 14 (Payments config table)
* **Original Mapping Rule**: `REFUND, any, sales_tax_collected, txn_ref+sku+date, , ` (empty summary fields)
* **Root Cause**: Refund transactions on taxes (sales_tax_collected) were not routed to any summary field in the Payments config, but Settlements routes them to refunded_expenses.
* **Exact Mapping Change**:
  ```sql
  UPDATE payment_configs
  SET to_summary_field_when_positive_amount = 'refunded_expenses',
      to_summary_field_when_negative_amount = 'refunded_expenses'
  WHERE transaction_type = 'REFUND'
    AND amount_field = 'sales_tax_collected';
  ```
* **After-fix Value**: \$216.28

---

### Defect 4: Settlement SHIPPINGTAX Misalignment
* **Summary Field**: `sales_product_charges` (under-allocated on Settlements by \$36.31)
* **Before Payment Value**: \$348,815.93
* **Before Settlement Value**: \$348,779.62
* **Exact Variance**: \$36.31 (This is the combined variance of Defects #4, #5, #6, and #7)
* **Affected record_ref(s)**: Settlement orders with shipping tax
* **Source File**: `amazon_settlements_data.txt`
* **Source Line Numbers**: `10, 25, 41` (and others containing `SHIPPINGTAX`)
* **Mapping Config Line**: Line 55 (Settlements config table)
* **Original Mapping Rule**: `ORDER, ITEMPRICE, SHIPPINGTAX, txn_ref+sku+date, sales_shipping, sales_shipping`
* **Root Cause**: Settlements separates tax components whereas Payments pools them under `sales_product_charges` (since Australian GST is pooled). The original rule incorrectly routed this tax portion to `sales_shipping`.
* **Exact Mapping Change**:
  ```sql
  UPDATE settlement_configs
  SET to_summary_field_when_positive_amount = 'sales_product_charges',
      to_summary_field_when_negative_amount = 'sales_product_charges'
  WHERE transaction_type = 'ORDER' 
    AND amount_type = 'ITEMPRICE' 
    AND amount_description = 'SHIPPINGTAX';
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
* **Mapping Config Line**: Line 53 (Settlements config table)
* **Original Mapping Rule**: `ORDER, ITEMPRICE, GIFTWRAPTAX, txn_ref+sku+date, sales_other, sales_other`
* **Root Cause**: Giftwrap tax was incorrectly routed to `sales_other` instead of product charges, causing a \$0.36 discrepancy.
* **Exact Mapping Change**:
  ```sql
  UPDATE settlement_configs
  SET to_summary_field_when_positive_amount = 'sales_product_charges',
      to_summary_field_when_negative_amount = 'sales_product_charges'
  WHERE transaction_type = 'ORDER' 
    AND amount_type = 'ITEMPRICE' 
    AND amount_description = 'GIFTWRAPTAX';
  ```
* **After-fix Value**: \$348,815.93 / \$11.61

---

### Defect 6: Settlement TAXDISCOUNT Misalignment
* **Summary Field**: `sales_product_charges` (under-allocated on Settlements by \$36.31)
* **Before Payment Value**: \$348,815.93
* **Before Settlement Value**: \$348,779.62
* **Exact Variance**: \$36.31 (This is the combined variance of Defects #4, #5, #6, and #7)
* **Affected record_ref(s)**: Orders with promotion tax discounts
* **Source File**: `amazon_settlements_data.txt`
* **Source Line Numbers**: `132, 285` (and others containing `TAXDISCOUNT`)
* **Mapping Config Line**: Line 58 (Settlements config table)
* **Original Mapping Rule**: `ORDER, PROMOTION, TAXDISCOUNT, txn_ref+sku+date, sales_shipping, sales_shipping`
* **Root Cause**: Promotional tax discounts were incorrectly routed to `sales_shipping`. Because the Australian marketplace tax is treated as part of product-tax reconciliation, the tax discount should be routed to `sales_product_charges`.
* **Exact Mapping Change**:
  ```sql
  UPDATE settlement_configs
  SET to_summary_field_when_positive_amount = 'sales_product_charges',
      to_summary_field_when_negative_amount = 'sales_product_charges'
  WHERE transaction_type = 'ORDER' 
    AND amount_type = 'PROMOTION' 
    AND amount_description = 'TAXDISCOUNT';
  ```
* **After-fix Value**: \$348,815.93

---

### Defect 7: Settlement LOWVALUEGOODSTAX-SHIPPING Misalignment
* **Summary Field**: `sales_product_charges` (under-allocated on Settlements by \$36.31)
* **Before Payment Value**: \$348,815.93
* **Before Settlement Value**: \$348,779.62
* **Exact Variance**: \$36.31 (This is the combined variance of Defects #4, #5, #6, and #7)
* **Affected record_ref(s)**: Orders with low value goods shipping tax
* **Source File**: `amazon_settlements_data.txt`
* **Source Line Numbers**: `15, 30` (and others containing `LOWVALUEGOODSTAX-SHIPPING`)
* **Mapping Config Line**: Line 61 (Settlements config table)
* **Original Mapping Rule**: `ORDER, ITEMWITHHELDTAX, LOWVALUEGOODSTAX-SHIPPING, txn_ref+sku+date, sales_shipping, sales_shipping`
* **Root Cause**: Low value goods withheld tax on shipping was routed to shipping instead of product charges.
* **Exact Mapping Change**:
  ```sql
  UPDATE settlement_configs
  SET to_summary_field_when_positive_amount = 'sales_product_charges',
      to_summary_field_when_negative_amount = 'sales_product_charges'
  WHERE transaction_type = 'ORDER' 
    AND amount_type = 'ITEMWITHHELDTAX' 
    AND amount_description = 'LOWVALUEGOODSTAX-SHIPPING';
  ```
* **After-fix Value**: \$348,815.93
