-- ==========================================
-- PortOne Reconciliation Mapping Adjustments
-- ==========================================

-- DEFECT #1 (Payments Config: Low Value Goods Rule)
-- Original rule: ORDER | low_value_goods | ANY -> sales_shipping (Line 5)
-- Observed variance: sales_shipping was over-allocated on Payments side by the tax amount.
-- Root cause: Low value goods tax represents product taxes and should only route to sales_product_charges.
-- Fix: Delete this rule so it doesn't duplicate the tax allocation on shipping.
DELETE FROM payment_configs 
WHERE transaction_type = 'ORDER' 
  AND amount_field = 'low_value_goods' 
  AND to_summary_field_when_positive_amount = 'sales_shipping';


-- DEFECT #2 (Payments Config: Sales Tax Collected Rule)
-- Original rule: ORDER | sales_tax_collected | ANY -> sales_shipping (Line 70)
-- Observed variance: sales_shipping was over-allocated on Payments side.
-- Root cause: This duplicate rule incorrectly routed sales_tax_collected to sales_shipping. Tax collected should only route to product charges.
-- Fix: Delete this rule to prevent shipping totals from being inflated by tax amounts.
DELETE FROM payment_configs 
WHERE transaction_type = 'ORDER' 
  AND amount_field = 'sales_tax_collected' 
  AND to_summary_field_when_positive_amount = 'sales_shipping';


-- DEFECT #3 (Payments Config: Refund Tax Rule)
-- Original rule: REFUND | any | sales_tax_collected -> (Empty summary fields) (Line 14)
-- Observed variance: refunded_expenses was under-allocated on the Payments side by the tax refund amount.
-- Root cause: Refund transactions on taxes (sales_tax_collected) were not routed to any summary field, but Settlements routes them to refunded_expenses.
-- Fix: Update the routing for the existing REFUND sales_tax_collected rule to refunded_expenses.
UPDATE payment_configs
SET to_summary_field_when_positive_amount = 'refunded_expenses',
    to_summary_field_when_negative_amount = 'refunded_expenses'
WHERE transaction_type = 'REFUND'
  AND amount_field = 'sales_tax_collected';


-- DEFECT #4 (Settlements Config: Shipping Tax Routing)
-- Original rule: ORDER | ITEMPRICE | SHIPPINGTAX -> sales_shipping
-- Observed variance: Shipping and product charges differed between Payments and Settlements.
-- Root cause: In GST-inclusive marketplaces like Australia, shipping tax is part of product charges. Payments routes it to sales_product_charges.
-- Fix: Update the routing for SHIPPINGTAX to sales_product_charges.
UPDATE settlement_configs
SET to_summary_field_when_positive_amount = 'sales_product_charges',
    to_summary_field_when_negative_amount = 'sales_product_charges'
WHERE transaction_type = 'ORDER' 
  AND amount_type = 'ITEMPRICE' 
  AND amount_description = 'SHIPPINGTAX';


-- DEFECT #5 (Settlements Config: Gift Wrap Tax Routing)
-- Original rule: ORDER | ITEMPRICE | GIFTWRAPTAX -> sales_other
-- Observed variance: sales_other vs sales_product_charges variance of 0.36.
-- Root cause: Giftwrap tax in GST-inclusive marketplaces is bundled into the single Payments tax column, which goes to sales_product_charges.
-- Fix: Update the routing for GIFTWRAPTAX to sales_product_charges.
UPDATE settlement_configs
SET to_summary_field_when_positive_amount = 'sales_product_charges',
    to_summary_field_when_negative_amount = 'sales_product_charges'
WHERE transaction_type = 'ORDER' 
  AND amount_type = 'ITEMPRICE' 
  AND amount_description = 'GIFTWRAPTAX';


-- DEFECT #6 (Settlements Config: Tax Discount Promotion Routing)
-- Original rule: ORDER | PROMOTION | TAXDISCOUNT -> sales_shipping
-- Observed variance: Mismatch in shipping and product charges totals.
-- Root cause: Promotional tax discounts on GST-inclusive sales are reductions of product tax, which goes to product charges.
-- Fix: Update the routing for TAXDISCOUNT to sales_product_charges.
UPDATE settlement_configs
SET to_summary_field_when_positive_amount = 'sales_product_charges',
    to_summary_field_when_negative_amount = 'sales_product_charges'
WHERE transaction_type = 'ORDER' 
  AND amount_type = 'PROMOTION' 
  AND amount_description = 'TAXDISCOUNT';


-- DEFECT #7 (Settlements Config: Low Value Goods Shipping Tax Routing)
-- Original rule: ORDER | ITEMWITHHELDTAX | LOWVALUEGOODSTAX-SHIPPING -> sales_shipping
-- Observed variance: Mismatch on low value goods shipping tax adjustments (0.77, 0.95, and 1.00).
-- Root cause: Since SHIPPINGTAX is routed to sales_product_charges, the withheld tax discount on shipping must also route to sales_product_charges so they offset.
-- Fix: Update the routing for LOWVALUEGOODSTAX-SHIPPING to sales_product_charges.
UPDATE settlement_configs
SET to_summary_field_when_positive_amount = 'sales_product_charges',
    to_summary_field_when_negative_amount = 'sales_product_charges'
WHERE transaction_type = 'ORDER' 
  AND amount_type = 'ITEMWITHHELDTAX' 
  AND amount_description = 'LOWVALUEGOODSTAX-SHIPPING';
